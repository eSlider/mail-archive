FROM python:3.12-slim

WORKDIR /app

RUN apt-get update && \
  apt-get install -y --no-install-recommends \
  tini \
  && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY scripts/ /app/scripts/

ENTRYPOINT ["tini", "--"]
CMD ["python", "/app/scripts/daemon.py"]
