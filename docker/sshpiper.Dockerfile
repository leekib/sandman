FROM alpine:latest

# SSHPiper 설치
RUN apk add --no-cache openssh-client curl

# SSHPiper 바이너리 다운로드
RUN curl -L https://github.com/tg123/sshpiper/releases/latest/download/sshpiper_linux_amd64.tar.gz | \
    tar -xz -C /usr/local/bin/

# 설정 디렉토리 생성
RUN mkdir -p /etc/sshpiper

# SSH 키 생성
RUN ssh-keygen -t rsa -b 2048 -f /etc/sshpiper/id_rsa -N ""

# 포트 노출
EXPOSE 22

# 시작 명령
CMD ["/usr/local/bin/sshpiper", "-c", "/etc/sshpiper"] 