FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal ./internal
COPY cmd/shared-todo ./cmd/shared-todo
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/shared-todo ./cmd/shared-todo

FROM scratch
COPY --from=build /out/shared-todo /shared-todo
COPY THIRD_PARTY_NOTICES.txt /licenses/THIRD_PARTY_NOTICES.txt
ENV PORT=8080 TODO_DATA_DIR=/data
EXPOSE 8080
ENTRYPOINT ["/shared-todo"]
