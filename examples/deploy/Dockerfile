FROM valontechnologies/gestaltd:latest AS build
COPY config.yaml /src/config.yaml
RUN ["/gestaltd", "bundle", "--config", "/src/config.yaml", "--output", "/app"]

FROM valontechnologies/gestaltd:latest
COPY --from=build /app/ /app/
CMD ["serve", "--locked", "--config", "/app/config.yaml"]
