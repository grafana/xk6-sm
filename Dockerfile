FROM grafana/synthetic-monitoring-agent:latest-browser

ENV K6_BROWSER_SCREENSHOTS_OUTPUT=url=http://127.0.0.1:2345
ENV K6_LOG_LEVEL=debug

ADD --chown=sm:sm --chmod=0500 ./dist/sm-k6-linux-arm64 /usr/local/bin/sm-k6
