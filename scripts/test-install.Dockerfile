FROM ubuntu:24.04
RUN apt-get update && apt-get install -y curl ca-certificates && rm -rf /var/lib/apt/lists/*
COPY scripts/install.sh /tmp/install.sh
RUN chmod +x /tmp/install.sh
# Test the install script (will fail until first release exists)
# RUN VERDA_VERSION=v1.0.0 sh /tmp/install.sh
CMD ["sh", "/tmp/install.sh"]
