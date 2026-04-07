FROM valontechnologies/gestaltd:latest AS init
USER root
COPY .gestaltd/config.yaml /app/.gestaltd/config.yaml
RUN ["/gestaltd", "init", "--config", "/app/.gestaltd/config.yaml"]

FROM valontechnologies/gestaltd:latest
COPY --from=init /app/.gestaltd/ /app/.gestaltd/
CMD ["serve", "--locked", "--config", "/app/.gestaltd/config.yaml"]
