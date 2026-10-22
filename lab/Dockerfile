FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/damocline ./cmd/damocline && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/damocline-calibrate ./cmd/damocline-calibrate && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/damocline-sensor ./cmd/damocline-sensor && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/damocline-labtarget ./cmd/damocline-labtarget

FROM alpine:3.22 AS target
RUN addgroup -S damocline && adduser -S -G damocline damocline
COPY --from=build /out/damocline-labtarget /usr/local/bin/damocline-labtarget
USER damocline
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/damocline-labtarget"]
CMD ["--listen", "0.0.0.0:8080", "--parallel", "1", "--queue", "16", "--delay", "100ms"]

FROM alpine:3.22 AS sensor
RUN addgroup -S damocline && adduser -S -G damocline damocline
COPY --from=build /out/damocline-sensor /usr/local/bin/damocline-sensor
USER damocline
ENTRYPOINT ["/usr/local/bin/damocline-sensor"]
CMD ["--listen", "127.0.0.1:9467"]

FROM alpine:3.22 AS generator
RUN addgroup -S damocline && adduser -S -G damocline damocline && mkdir /reports && chown damocline:damocline /reports
COPY --from=build /out/damocline /usr/local/bin/damocline
COPY --from=build /out/damocline-calibrate /usr/local/bin/damocline-calibrate
COPY lab/scenario.json /lab/
COPY lab/scenario-exploration.json /lab/
COPY lab/scenario-overload.json /lab/
COPY lab/scenario-wordpress.json /lab/
USER damocline
ENTRYPOINT ["/usr/local/bin/damocline"]
