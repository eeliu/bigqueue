package bigqueue

import (
	"fmt"
	"os"
	"path"
	"syscall"

	"github.com/grandecola/mmap"
)

const (
	cArenaFileSuffix = "_arena.dat"
)

// metadataProvider defines the interface for metadata operations needed by arenaManager.
type metadataProvider interface {
	getTail() (int, int)
	getHead() (int, int)
	putHead(aid, off int)
	flush() error
	getConsumerHead(addr int64) (int, int)
	getConsumers() map[string]int64
}

// arenaManager manages all the arenas for a bigqueue
type arenaManager struct {
	dir    string
	conf   *bqConfig
	md     metadataProvider
	arenas map[int]*mmap.File
	inMem  int
}

// newArenaManager returns a pointer to new arenaManager.
func newArenaManager(dir string, conf *bqConfig, md metadataProvider) (*arenaManager, error) {
	tailAid, _ := md.getTail()

	am := &arenaManager{
		dir:    path.Clean(dir),
		conf:   conf,
		md:     md,
		arenas: make(map[int]*mmap.File),
	}

	conf.Infof("initializing arena manager; dir=%s, tail_aid=%d", am.dir, tailAid)

	// we load the tail arena into memory
	if err := am.loadArena(tailAid); err != nil {
		conf.Errorf("failed to load tail arena; aid=%d, error=%v", tailAid, err)
		return nil, err
	}

	am.gc()

	return am, nil
}

// gc deletes the arena files that are no longer needed.
func (m *arenaManager) gc() {
	if m.conf.maxArenasToKeep <= 0 {
		return
	}

	// find the minimum consumer head aid
	minHeadAid, minHeadOff := -1, -1
	for _, addr := range m.md.getConsumers() {
		aid, off := m.md.getConsumerHead(addr)
		if minHeadAid == -1 || aid < minHeadAid || (aid == minHeadAid && off < minHeadOff) {
			minHeadAid = aid
			minHeadOff = off
		}
	}

	if minHeadAid == -1 {
		return
	}

	// update global head to the minimum among all consumers.
	// this is to ensure new consumers start from the earliest available data.
	m.md.putHead(minHeadAid, minHeadOff)
	if err := m.md.flush(); err != nil {
		m.conf.Warnf("failed to flush metadata after updating global head; error=%v", err)
		return
	}

	// we keep maxArenasToKeep arenas before the minHeadAid
	// everything before (minHeadAid - maxArenasToKeep) can be deleted.
	limitAid := minHeadAid - m.conf.maxArenasToKeep
	if limitAid <= 0 {
		return
	}

	m.conf.Infof("garbage collection started; min_head_aid=%d, limit_aid=%d", minHeadAid, limitAid)

	// startAid - we can delete from 0 up to limitAid-1.
	for aid := 0; aid < limitAid; aid++ {
		// remove from memory if loaded
		if aa, ok := m.arenas[aid]; ok && aa != nil {
			_ = m.unloadArena(aid)
		}
		delete(m.arenas, aid)

		arenaPath := m.getArenaPath(aid)
		if _, err := os.Stat(arenaPath); err == nil {
			if err := os.Remove(arenaPath); err != nil {
				m.conf.Errorf("failed to delete arena file; aid=%d, path=%s, error=%v", aid, arenaPath, err)
			} else {
				m.conf.Infof("arena file deleted; aid=%d, path=%s", aid, arenaPath)
			}
		}
	}
}

// getArenaPath returns the full path for a given arena ID.
func (m *arenaManager) getArenaPath(aid int) string {
	fileName := fmt.Sprintf("%d%s", aid, cArenaFileSuffix)
	return path.Join(m.dir, fileName)
}

// loadOrGetArena returns arena for a given arena ID
func (m *arenaManager) loadOrGetArena(aid int) (*mmap.File, error) {
	if aa, ok := m.arenas[aid]; ok && aa != nil {
		return aa, nil
	}

	m.conf.Debugf("loading or creating arena; aid=%d", aid)

	// if this is a new arena being requested (tail expansion)
	// getTail doesn't help here because writer might be calling it before metadata update
	// but basically if it's not in the map, we try to load or create it.
	// In the original slice logic, relAid == len(m.arenas) triggered GC.
	// Here we can check if it's beyond a certain aid or just trigger GC occasionally.

	// before we get a new arena into memory, we need to ensure that after fetching
	// a new arena into memory, we do not cross the provided memory limit.
	if err := m.ensureEnoughMem(); err != nil {
		m.conf.Errorf("failed to ensure enough memory for arena; aid=%d, error=%v", aid, err)
		return nil, err
	}

	// now, get arena into memory
	if err := m.loadArena(aid); err != nil {
		m.conf.Errorf("failed to load arena; aid=%d, error=%v", aid, err)
		return nil, err
	}

	m.gc()

	return m.arenas[aid], nil
}

// ensureEnoughMem ensures that at least 1 new arena can be brought into memory.
func (m *arenaManager) ensureEnoughMem() error {
	// if no limit on # of arenas, no need for eviction.
	if m.conf.maxInMemArenas == 0 {
		return nil
	}

	// Check whether an eviction is needed to begin with.
	if m.inMem < m.conf.maxInMemArenas {
		return nil
	}

	m.conf.Infof("memory limit reached, evicting arenas; in_mem_arenas=%d, max_in_mem_arenas=%d", m.inMem, m.conf.maxInMemArenas)

	// Start evicting from the arena just before the last arena that we have.
	// If message size > arena size, last arena may not always be the tail arena.
	// We always ensure that head and tail arenas are not evicted from memory.
	// Simply iterate from the last arena until enough memory is
	// available for a new arena to be loaded into memory
	tailAid, _ := m.md.getTail()
	headAid, _ := m.md.getHead()
	for aid, aa := range m.arenas {
		if m.inMem < m.conf.maxInMemArenas {
			break
		}

		if aid == tailAid || aid == headAid || aa == nil {
			continue
		}

		m.conf.Debugf("evicting arena from memory; aid=%d", aid)
		if err := m.unloadArena(aid); err != nil {
			m.conf.Errorf("failed to unload arena during eviction; aid=%d, error=%v", aid, err)
			return err
		}
	}

	return nil
}

// loadArena will fetch the arena into memory.
func (m *arenaManager) loadArena(aid int) error {
	if aa, ok := m.arenas[aid]; ok && aa != nil {
		return nil
	}

	arenaPath := m.getArenaPath(aid)
	m.conf.Debugf("opening arena file; aid=%d, path=%s", aid, arenaPath)
	aa, err := newArena(arenaPath, m.conf.arenaSize)
	if err != nil {
		return err
	}

	m.inMem++
	m.arenas[aid] = aa
	return nil
}

// unloadArena will remove the arena from memory.
func (m *arenaManager) unloadArena(aid int) error {
	aa, ok := m.arenas[aid]
	if !ok || aa == nil {
		return nil
	}

	m.conf.Debugf("unmapping arena; aid=%d", aid)
	if err := aa.Unmap(); err != nil {
		return fmt.Errorf("error in unmap :: %w", err)
	}

	m.inMem--
	delete(m.arenas, aid)
	return nil
}

func (m *arenaManager) flush() error {
	for _, aa := range m.arenas {
		if aa == nil {
			continue
		}

		if err := aa.Flush(syscall.MS_SYNC); err != nil {
			return fmt.Errorf("error in flushing arena file :: %w", err)
		}
	}

	if err := m.md.flush(); err != nil {
		return err
	}

	return nil
}

// close unmaps all the arenas managed by arenaManager.
func (m *arenaManager) close() error {
	var retErr error
	for id, aa := range m.arenas {
		if aa == nil {
			continue
		}

		if err := aa.Unmap(); err != nil {
			retErr = err
		}
		delete(m.arenas, id)
	}

	if retErr != nil {
		return fmt.Errorf("error in unmap :: %w", retErr)
	}

	return nil
}
