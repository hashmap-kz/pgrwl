# Separate stage to cache mc binary download
FROM alpine AS mc-downloader

ARG MC_VERSION=mc.RELEASE.2025-08-13T08-35-41Z
RUN wget -q https://dl.min.io/client/mc/release/linux-amd64/${MC_VERSION} \
    -O /minio-mc && chmod +x /minio-mc

FROM debian:bookworm

COPY --from=mc-downloader /minio-mc /usr/local/bin/minio-mc

# Environment
ENV DEBIAN_FRONTEND=noninteractive
ENV PG_MAJOR=17

######################################################################
# Install PostgreSQL and other utilities

RUN apt-get update && \
    apt-get install -y wget curl gnupg lsb-release ca-certificates && \
    apt-get update && \
    apt-get install -y postgresql-common openssh-server sudo vim net-tools telnet mc && \
    apt-get clean && \
    mkdir /var/run/sshd

RUN apt install -y postgresql-common && \
    YES=true /usr/share/postgresql-common/pgdg/apt.postgresql.org.sh && \
    apt install -y postgresql-${PG_MAJOR} postgresql-client-${PG_MAJOR}

RUN rm -rf "/var/lib/postgresql/${PG_MAJOR}" && mkdir -p "/var/lib/postgresql/${PG_MAJOR}/main/pgdata"

######################################################################
## TZ settings

ENV TZ=Asia/Aqtau
RUN echo $TZ > /etc/timezone && \
    ln -sf /usr/share/zoneinfo/$TZ /etc/localtime && \
    apt-get install -y tzdata && dpkg-reconfigure -f noninteractive tzdata

######################################################################
# SSH configs

RUN sed -i 's/#PermitRootLogin prohibit-password/PermitRootLogin yes/' /etc/ssh/sshd_config && \
    sed -i 's/#PubkeyAuthentication yes/PubkeyAuthentication yes/' /etc/ssh/sshd_config

######################################################################
# Generic configs

RUN echo "export PATH=/usr/lib/postgresql/${PG_MAJOR}/bin:${PATH}" > /etc/profile.d/utils.sh
ENV PATH="/usr/lib/postgresql/${PG_MAJOR}/bin:${PATH}"

######################################################################
# PostgreSQL configs

COPY files/postgres/ /etc/postgresql/${PG_MAJOR}/main

######################################################################
# Setup 'postgres' user

RUN echo "postgres:postgres" | chpasswd
RUN echo "postgres ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/postgres
RUN mkdir -p /var/lib/postgresql/.ssh && chmod 700 /var/lib/postgresql/.ssh
COPY files/dotfiles/ /var/lib/postgresql
RUN chmod 600 /var/lib/postgresql/.ssh/authorized_keys \
    && chmod 600 /var/lib/postgresql/.ssh/id_ed25519 \
    && chown -R postgres:postgres /var/lib/postgresql \
    && chown -R postgres:postgres /etc/postgresql \
    && chown -R postgres:postgres /var/log/postgresql

######################################################################
# Setup 'root' user

RUN echo "root:root" | chpasswd && mkdir -p /root/.ssh && chmod 700 /root/.ssh
COPY files/dotfiles/ /root

######################################################################
# Execs

COPY bin/ /usr/local/bin
COPY files/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

######################################################################
# Copy test scripts (these files are frequently changed, so it's better to make this layer as late as possible)

COPY scripts /var/lib/postgresql/scripts
COPY files/configs /var/lib/postgresql/configs
RUN chown -R postgres:postgres /var/lib/postgresql

######################################################################
# Run container

EXPOSE 5432 22
ENTRYPOINT ["/entrypoint.sh"]
