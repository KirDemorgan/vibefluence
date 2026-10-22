FROM node:24-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.27-alpine AS backend
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=frontend /src/frontend/dist/vibefluence-frontend/browser/ ./internal/web/dist/
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/vibefluence ./cmd/vibefluence \
    && mkdir /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=backend /out/vibefluence /vibefluence
COPY --from=backend --chown=65532:65532 /out/data /data
ENV VIBEFLUENCE_DATA_DIR=/data
VOLUME ["/data"]
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/vibefluence"]
