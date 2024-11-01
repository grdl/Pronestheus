FROM golang:1.18-alpine
WORKDIR /app
COPY . .
RUN go build -o pronestheus ./cmd/pronestheus/main.go
RUN chmod +x /app/pronestheus
CMD ["/app/pronestheus"]

