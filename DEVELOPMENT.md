# Development Notes

## Building the Agent

1. Build the k6 binary:
```bash
docker run --rm -i -u "$(id -u):$(id -g)" -v ".:/xk6" \
    -e "GOOS=linux" -e "GOARCH=arm64" \
    grafana/xk6 build \
    --output "dist/sm-k6-linux-arm64" \
    --with github.com/grafana/xk6-sm=. \
    --with github.com/grafana/gsm-api-go-client
```

2. Build the Docker image:
```bash
docker build . -t synthetic-monitoring-agent-richvisuals
```

## Running the Agent

Run the agent with:
```bash
docker run synthetic-monitoring-agent-richvisuals \
    --api-server-address=synthetic-monitoring-grpc-dev.grafana-dev.net:443 \
    --api-token="token" \
    --verbose=true \
    --debug=true
``` 