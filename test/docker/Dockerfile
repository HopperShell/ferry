FROM alpine:latest

RUN apk add --no-cache openssh-server rsync && \
    ssh-keygen -A && \
    mkdir -p /root/.ssh && \
    echo "root:ferry" | chpasswd && \
    sed -i 's/#PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config && \
    sed -i 's/#PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config && \
    mkdir -p /testdata/projects /testdata/logs && \
    echo "hello from ferry test server" > /testdata/README.txt && \
    echo "some config" > /testdata/config.yml && \
    dd if=/dev/urandom of=/testdata/largefile.bin bs=1M count=5 2>/dev/null && \
    echo "project file 1" > /testdata/projects/app.js && \
    echo "project file 2" > /testdata/projects/style.css && \
    echo "log entry" > /testdata/logs/app.log

EXPOSE 22

CMD ["/usr/sbin/sshd", "-D", "-e"]
