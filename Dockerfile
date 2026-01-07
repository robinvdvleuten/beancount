FROM scratch

ARG TARGETPLATFORM

ENTRYPOINT ["/usr/bin/beancount"]

COPY $TARGETPLATFORM/beancount /usr/bin/
