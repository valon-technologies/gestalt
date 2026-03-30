FROM valontechnologies/gestaltd:builder AS bundle
COPY config.yaml /src/config.yaml
RUN ["/gestaltd", "bundle", "--config", "/src/config.yaml", "--output", "/app"]

FROM valontechnologies/gestaltd:latest
COPY --from=bundle /app/ /app/
CMD ["serve", "--locked", "--config", "/app/config.yaml"]
