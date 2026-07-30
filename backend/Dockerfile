FROM golang:1.24 AS build
WORKDIR /app
COPY . .
RUN go build -o server ./cmd/server
CMD ["./server"]
