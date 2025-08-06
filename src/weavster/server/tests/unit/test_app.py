from fastapi.testclient import TestClient

from weavster.server.app import app

client = TestClient(app)


def test_app_initialization():
    """Test that the FastAPI app is properly initialized."""
    assert app.title == "Weavster API"
    assert app.description == "Cloud-native integration platform API"
    assert app.version == "0.1.0"


def test_health_check_endpoint():
    """Test the health check endpoint returns correct response."""
    response = client.get("/health")

    assert response.status_code == 200
    assert response.headers["content-type"] == "application/json"

    data = response.json()
    assert data["status"] == "healthy"
    assert data["service"] == "weavster-api"


def test_health_check_endpoint_structure():
    """Test that health check response has expected structure."""
    response = client.get("/health")
    data = response.json()

    # Verify required fields are present
    assert "status" in data
    assert "service" in data

    # Verify field types
    assert isinstance(data["status"], str)
    assert isinstance(data["service"], str)


def test_nonexistent_endpoint():
    """Test that nonexistent endpoints return 404."""
    response = client.get("/nonexistent")

    assert response.status_code == 404


def test_health_check_method_not_allowed():
    """Test that non-GET methods on health endpoint return 405."""
    response = client.post("/health")
    assert response.status_code == 405

    response = client.put("/health")
    assert response.status_code == 405

    response = client.delete("/health")
    assert response.status_code == 405
