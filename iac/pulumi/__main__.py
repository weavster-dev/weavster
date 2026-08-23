# Sample Pulumi program provisioning Weavster (mirrors the Terraform sample).
# Install with: pulumi up

import pulumi

config = pulumi.Config()
database_url = config.require("databaseUrl")
config_repo = config.require("configRepo")
tls_cert_arn = config.require("tlsCertArn")
enable_mtls = config.get_bool("enableMtls", True)
metrics_enabled = config.get_bool("metricsEnabled", True)

container = {
    "name": "weavster",
    "image": "weavster:latest",
    "ports": [{"internal": 8080, "external": 8080}],
    "envs": [
        f"WEAVSTER_DATABASE_URL={database_url}",
        f"WEAVSTER_CONFIG_REPO={config_repo}",
        f"WEAVSTER_TLS_CERT_ARN={tls_cert_arn}",
        f"WEAVSTER_ENABLE_MTLS={enable_mtls}",
        f"WEAVSTER_METRICS_ENABLED={metrics_enabled}",
    ],
}

pulumi.export("container", container)
