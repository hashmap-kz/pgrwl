FROM alpine:3.22.2

# Install OpenSSH
RUN apk update && apk add --no-cache openssh sudo bash mc vim inetutils-telnet

# For shell PS1
RUN echo "export PS1='\\u@\\h:\\w\\$ '" >> /etc/profile

# Setup 'root' user
RUN echo "root:root" | chpasswd
COPY files/dotfiles/ /root

# Create user
RUN adduser -D -s /bin/bash testuser && echo "testuser:testuser" | chpasswd

# Add user to wheel group for sudo access
RUN addgroup testuser wheel

# Enable passwordless sudo for wheel group
RUN echo "%wheel ALL=(ALL) NOPASSWD: ALL" >> /etc/sudoers

# Allow password authentication and root login
RUN sed -i 's/#PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config && \
    sed -i 's/#PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config && \
    sed -i 's/#PubkeyAuthentication.*/PubkeyAuthentication yes/' /etc/ssh/sshd_config

# Generate SSH host keys
RUN ssh-keygen -A

# Setup 'testuser'
COPY files/dotfiles/ /home/testuser
RUN chmod 700 /home/testuser/.ssh && \
    chmod 600 /home/testuser/.ssh/authorized_keys && \
    chown -R testuser:testuser /home/testuser

# Expose SSH port
EXPOSE 22

# Run SSH daemon
CMD ["/usr/sbin/sshd", "-D"]
