# ---------- Dockerfile ----------
    FROM nvidia/cuda:12.9.1-devel-ubuntu24.04

    # 선택: 빌드 타임에 사용자·그룹·키 지정
    ARG USERNAME=user
    ARG UID=1001
    ARG GID=1001
    ARG PUBKEY="ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJkhYlvA41Rka3VfOQi2ZM4ZEb7Za7I+hYUZA9V3pwZ9 leekib@sandbox"
    
    # 기본 패키지 설치
    RUN apt-get update && \
        apt-get install -y --no-install-recommends openssh-server sudo && \
        rm -rf /var/lib/apt/lists/* && \
        mkdir /var/run/sshd
    
    # 비-root 사용자 생성
    RUN groupadd -g ${GID} ${USERNAME} && \
        useradd  -m -u ${UID} -g ${GID} -s /bin/bash ${USERNAME} && \
        echo "${USERNAME} ALL=(ALL) NOPASSWD:ALL" >> /etc/sudoers
    
    # SSH 설정: public key 배치, 패스워드 로그인 차단
    RUN mkdir -p /home/${USERNAME}/.ssh && \
        chmod 700 /home/${USERNAME}/.ssh && \
        if [ -n "$PUBKEY" ]; then \
            echo "$PUBKEY" > /home/${USERNAME}/.ssh/authorized_keys ; \
        fi && \
        chown -R ${USERNAME}:${USERNAME} /home/${USERNAME}/.ssh && \
        chmod 600 /home/${USERNAME}/.ssh/authorized_keys && \
        sed -i 's/#PasswordAuthentication yes/PasswordAuthentication no/' /etc/ssh/sshd_config && \
        sed -i 's/#PermitRootLogin prohibit-password/PermitRootLogin no/' /etc/ssh/sshd_config
    
    EXPOSE 22
    CMD ["/usr/sbin/sshd","-D"]
    