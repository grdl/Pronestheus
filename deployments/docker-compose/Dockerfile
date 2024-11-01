FROM golang:1.18
WORKDIR /app
COPY . .
RUN go build -o pronestheus ./pkg  # Adjusting to build from the pkg directory where pronestheus.go is located
CMD ["/app/pronestheus"]
