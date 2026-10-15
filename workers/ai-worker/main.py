from fastapi import FastAPI
import uvicorn

app = FastAPI(title="AI Worker")


@app.get("/health")
def health():
    return {"status": "healthy", "service": "ai-worker"}


@app.get("/")
def root():
    return {"message": "AI Worker - Root cause analysis service"}


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=8084)
