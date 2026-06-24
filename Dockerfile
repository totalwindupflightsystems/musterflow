# MusterFlow Docker image
#
# Build the binary first (requires local muster engine):
#   CGO_ENABLED=0 go build -ldflags="-s -w" -o musterflow ./cmd/musterflow/
#
# Then build the image:
#   docker build -t musterflow .
#
# Or use docker buildx for multi-arch:
#   docker buildx build --platform linux/amd64,linux/arm64 -t ghcr.io/totalwindupflightsystems/musterflow:latest .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates

COPY musterflow /usr/local/bin/musterflow

EXPOSE 9876
VOLUME /root/.musterflow

ENTRYPOINT ["musterflow"]
CMD ["start"]
