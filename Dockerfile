FROM golang:1.18
WORKDIR /app
COPY . .
RUN GOOS=linux GOARCH=amd64 go build -o pronestheus ./pkg
RUN chmod +x /app/pronestheus
CMD ["/app/pronestheus"]
