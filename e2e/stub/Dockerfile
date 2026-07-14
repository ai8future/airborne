FROM python:3.13.1-alpine@sha256:f9d772b2b40910ee8de2ac2b15ff740b5f26b37fc811f6ada28fce71a2542b0e
WORKDIR /app
COPY server.py .
COPY certs/ ./certs/
EXPOSE 8080 8443
CMD ["python", "server.py"]
