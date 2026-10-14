# Build web/ then the static Go binary with -tags release on a workstation/CI.
FROM gcr.io/distroless/static-debian12:nonroot
ARG VCS_REF=development
LABEL org.opencontainers.image.source="https://github.com/EthanHannn/personal-library"
LABEL org.opencontainers.image.revision=$VCS_REF
COPY --chown=65532:65532 bin/library-linux-amd64 /library
USER 65532:65532
ENV GOMEMLIMIT=160MiB
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s CMD ["/library", "healthcheck"]
ENTRYPOINT ["/library"]
CMD ["serve", "--listen", "0.0.0.0:8080", "--db", "/data/library/library.db", "--novels", "/data/novels"]
