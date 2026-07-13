FROM python:3.13.1-alpine
WORKDIR /app
COPY server.py .
COPY certs/ ./certs/
EXPOSE 8080 8443
CMD ["python", "server.py"]
