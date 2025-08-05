"""FastAPI server application for Weavster."""

from fastapi import FastAPI
from fastapi.responses import JSONResponse

app = FastAPI(
    title="Weavster API",
    description="Cloud-native integration platform API",
    version="0.1.0",
)


@app.get("/health")
async def health_check() -> JSONResponse:
    """Health check endpoint."""
    return JSONResponse({"status": "healthy", "service": "weavster-api"})
