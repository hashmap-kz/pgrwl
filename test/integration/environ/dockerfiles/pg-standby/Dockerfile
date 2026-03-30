ARG PG_MAJOR=17
FROM postgres:${PG_MAJOR}-alpine3.22

RUN sed -i 's/https/http/' /etc/apk/repositories

ENV PG_MAJOR=${PG_MAJOR}
