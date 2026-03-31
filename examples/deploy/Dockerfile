FROM valontechnologies/gestaltd:latest AS init
USER root
COPY config.yaml /app/config.yaml
RUN ["/gestaltd", "init", "--config", "/app/config.yaml"]

FROM valontechnologies/gestaltd:latest
COPY --from=init /app/ /app/
CMD ["serve", "--locked", "--config", "/app/config.yaml"]
