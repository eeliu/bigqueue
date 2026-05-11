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

// arenaManager manages all the arenas for a bigqueue
type arenaManager struct {
	dir    string
	conf   *bqConfig
	md     *metadata
	arenas map[int]*mmap.File
	inMem  int
}

// newArenaManager returns a pointer to new arenaManager.
func newArenaManager(dir string, conf *bqConfig, md *metadata) (*arenaManager, error) {
	tailAid, _ := md.getTail()

	am := &arenaManager{
		dir:    path.Clean(dir),
		conf:   conf,
		md:     md,
		arenas: make(map[int]*mmap.File),
	}

	conf.Info("initializing arena manager", "dir", am.dir, "tail_aid", tailAid)

	// we load the tail arena into memory
	if err := am.loadArena(tailAid); err != nil {
		conf.Error("failed to load tail arena", "aid", tailAid, "error", err)
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
	for _, base := range m.md.co {
		aid, off := m.md.getConsumerHead(base)
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
	_ = m.md.flush()

	// we keep maxArenasToKeep arenas before the minHeadAid
	// everything before (minHeadAid - maxArenasToKeep) can be deleted.
	limitAid := minHeadAid - m.conf.maxArenasToKeep
	if limitAid <= 0 {
		return
	}

	m.conf.Info("garbage collection started", "min_head_aid", minHeadAid, "limit_aid", limitAid)

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
				m.conf.Error("failed to delete arena file", "aid", aid, "path", arenaPath, "error", err)
			} else {
				m.conf.Info("arena file deleted", "aid", aid, "path", arenaPath)
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

	m.conf.Debug("loading or creating arena", "aid", aid)

	// if this is a new arena being requested (tail expansion)
	// getTail doesn't help here because writer might be calling it before metadata update
	// but basically if it's not in the map, we try to load or create it.
	// In the original slice logic, relAid == len(m.arenas) triggered GC.
	// Here we can check if it's beyond a certain aid or just trigger GC occasionally.

	// before we get a new arena into memory, we need to ensure that after fetching
	// a new arena into memory, we do not cross the provided memory limit.
	if err := m.ensureEnoughMem(); err != nil {
		m.conf.Error("failed to ensure enough memory for arena", "aid", aid, "error", err)
		return nil, err
	}

	// now, get arena into memory
	if err := m.loadArena(aid); err != nil {
		m.conf.Error("failed to load arena", "aid", aid, "error", err)
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

	m.conf.Info("memory limit reached, evicting arenas", "in_mem_arenas", m.inMem, "max_in_mem_arenas", m.conf.maxInMemArenas)

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

		m.conf.Debug("evicting arena from memory", "aid", aid)
		if err := m.unloadArena(aid); err != nil {
			m.conf.Error("failed to unload arena during eviction", "aid", aid, "error", err)
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
	m.conf.Debug("opening arena file", "aid", aid, "path", arenaPath)
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

	m.conf.Debug("unmapping arena", "aid", aid)
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
