FROM golang:1.25-bookworm

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        git \
        sudo \
    && rm -rf /var/lib/apt/lists/*

# Create a non-root user 'ubuntu' with sudo access
RUN groupadd --gid 1000 ubuntu \
    && useradd --uid 1000 --gid 1000 -m ubuntu -s /bin/bash \
    && echo ubuntu ALL=\(root\) NOPASSWD:ALL > /etc/sudoers.d/ubuntu \
    && chmod 0440 /etc/sudoers.d/ubuntu

USER ubuntu
ENV SHELL /bin/bash
WORKDIR /workspaces/bigqueue

# Set standard bash prompt to show directory
RUN echo "export PS1='\\u@\\h:\\w\\$ '" >> /home/ubuntu/.bashrc

