FROM python:3.11-slim
WORKDIR /app
RUN pip install --no-cache-dir numpy==2.3.5 onnxruntime==1.23.2 tokenizers==0.22.1
COPY server.py /app/server.py
ENV MODEL_DIR=/models
EXPOSE 8000
CMD ["python", "/app/server.py"]
