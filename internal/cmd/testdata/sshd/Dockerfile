FROM alpine:3.21
RUN apk add --no-cache openssh docker-cli bash coreutils && \
    ssh-keygen -A && \
    mkdir -p /root/.ssh && chmod 700 /root/.ssh && \
    sed -i 's/#PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config && \
    echo "PubkeyAuthentication yes" >> /etc/ssh/sshd_config && \
    echo "PasswordAuthentication no" >> /etc/ssh/sshd_config && \
    # Use bash as default shell — remote provider uses brace expansion
    sed -i 's|root:x:0:0:root:/root:.*|root:x:0:0:root:/root:/bin/bash|' /etc/passwd
EXPOSE 22
CMD ["/usr/sbin/sshd", "-D", "-e"]
