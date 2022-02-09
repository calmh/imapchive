FROM alpine:latest
COPY imapchive-linux-amd64 /bin/imapchive
ENTRYPOINT ["/bin/imapchive"]
