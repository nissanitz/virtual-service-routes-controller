FROM golang:1.12 AS build
WORKDIR /virtual-service-routes-controller
COPY . /virtual-service-routes-controller/
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -a -installsuffix cgo -o main .

RUN apt-get update && apt-get install -y upx
RUN upx main

RUN mkdir -p /empty

FROM scratch
COPY --from=build /virtual-service-routes-controller/main /
COPY --from=build /empty /tmp
CMD ["/main"]
