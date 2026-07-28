# build
FROM golang:1.25 AS build
WORKDIR /pam
COPY go.* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o server ./cmd

# run
FROM alpine:3.20
WORKDIR /pam
COPY --from=build /pam/server .
EXPOSE 8080
CMD ["./server"]