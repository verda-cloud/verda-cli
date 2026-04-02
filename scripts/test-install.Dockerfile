FROM ubuntu:24.04
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates && rm -rf /var/lib/apt/lists/*
RUN useradd -m testuser
USER testuser
COPY scripts/install.sh /tmp/install.sh
CMD ["sh", "/tmp/install.sh"]
