FROM bats/bats:1.6.0

RUN printf '\e[1;32m%-6s\e[m\n' "  - Installing OS deps"\ 
  && apk --no-cache --update add \
    curl

WORKDIR /opt/tests

RUN printf '\e[1;32m%-6s\e[m\n' "  - Installing kubectl" \
    && curl -LO "https://dl.k8s.io/release/v1.20.0/bin/linux/amd64/kubectl" \
    && chmod +x kubectl \
    && mv -v ./kubectl /usr/local/bin/