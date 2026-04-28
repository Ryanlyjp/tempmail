# ============================================================
# Postfix 收件容器
# ============================================================
FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=Asia/Shanghai

RUN apt-get update && apt-get install -y --no-install-recommends \
    postfix \
    python3 \
    curl \
    ca-certificates \
    tzdata \
    && rm -rf /var/lib/apt/lists/*

# 复制配置
COPY main.cf /etc/postfix/main.cf
COPY master.cf /etc/postfix/master.cf
COPY mail-receiver.py /usr/local/bin/mail-receiver
COPY entrypoint.sh /entrypoint.sh

RUN chmod +x /usr/local/bin/mail-receiver /entrypoint.sh

# 创建虚拟域名目录
RUN touch /etc/postfix/virtual_domains && postmap /etc/postfix/virtual_domains

EXPOSE 25

CMD ["/entrypoint.sh"]
