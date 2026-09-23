package api

const openapiJSON = `{
  "openapi": "3.0.3",
  "info": {
    "title": "Last State Trace API",
    "version": "1.0.0",
    "description": "Observability for embedded firmware — ingest, issues, devices, releases, alerts, analytics.",
    "contact": { "name": "Last State", "url": "https://laststate.dev" }
  },
  "servers": [{ "url": "/", "description": "Current server" }],
  "tags": [
    { "name": "health", "description": "Health & metrics" },
    { "name": "relay", "description": "Relay & ingest" },
    { "name": "auth", "description": "Authentication & sessions" },
    { "name": "issues", "description": "Issue management" },
    { "name": "events", "description": "Event pipeline" },
    { "name": "devices", "description": "Device management" },
    { "name": "releases", "description": "Release tracking" },
    { "name": "artifacts", "description": "Artifact store" },
    { "name": "orgs", "description": "Organization & membership" },
    { "name": "projects", "description": "Project management" },
    { "name": "alerts", "description": "Alert rules & notifications" },
    { "name": "channels", "description": "Notification channels" },
    { "name": "admin", "description": "Admin & billing" },
    { "name": "analytics", "description": "Analytics export" }
  ],
  "paths": {
    "/health/live": {
      "get": {
        "tags": ["health"],
        "summary": "Liveness probe",
        "responses": { "200": { "description": "ok" } }
      }
    },
    "/health/ready": {
      "get": {
        "tags": ["health"],
        "summary": "Readiness probe with component checks",
        "responses": {
          "200": { "description": "ok" },
          "503": { "description": "not ready — checks field lists failed components" }
        }
      }
    },
    "/metrics": {
      "get": {
        "tags": ["health"],
        "summary": "Prometheus metrics endpoint"
      }
    },
    "/openapi.json": {
      "get": {
        "tags": ["health"],
        "summary": "This OpenAPI document"
      }
    },
    "/v1/relay/capabilities": {
      "get": {
        "tags": ["relay"],
        "summary": "Relay capabilities (advertised by the server)",
        "responses": {
          "200": {
            "description": "Server capabilities",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "api_version": { "type": "string" },
                    "lep_versions": { "type": "array", "items": { "type": "integer" } },
                    "max_event_size": { "type": "integer" },
                    "compression": { "type": "array", "items": { "type": "string" } },
                    "authentication": { "type": "array", "items": { "type": "string" } }
                  }
                }
              }
            }
          }
        }
      }
    },
    "/v1/ingest": {
      "post": {
        "tags": ["relay"],
        "summary": "Ingest a single LEP event",
        "security": [{ "bearerAuth": [] }],
        "parameters": [
          { "name": "Idempotency-Key", "in": "header", "schema": { "type": "string" }, "description": "Idempotency key for retry safety" },
          { "name": "X-Last-State-Event-ID", "in": "header", "schema": { "type": "string" }, "description": "Pre-assigned event ID" }
        ],
        "requestBody": {
          "content": {
            "application/octet-stream": {
              "schema": { "type": "string", "format": "binary" },
              "description": "LEP-encoded event payload"
            }
          }
        },
        "responses": {
          "202": { "description": "Accepted or idempotent duplicate" },
          "400": { "description": "Invalid body" },
          "401": { "description": "Unauthorized" },
          "403": { "description": "Missing scope" },
          "413": { "description": "Event too large" },
          "422": { "description": "Conflict: same event_id, different payload" }
        }
      }
    },
    "/v1/events:batch": {
      "post": {
        "tags": ["relay"],
        "summary": "Batch ingest (up to 50 events)",
        "security": [{ "bearerAuth": [] }],
        "responses": {
          "200": { "description": "Batch result with accepted/duplicates/rejected" }
        }
      }
    },
    "/v1/artifacts": {
      "post": {
        "tags": ["relay"],
        "summary": "Upload debug symbol artifact",
        "security": [{ "bearerAuth": [] }],
        "responses": {
          "201": { "description": "Artifact stored" },
          "422": { "description": "Invalid artifact" }
        }
      }
    },
    "/v1/relay/heartbeat": {
      "post": {
        "tags": ["relay"],
        "summary": "Relay heartbeat",
        "security": [{ "bearerAuth": [] }],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "relay_id": { "type": "string" },
                  "name": { "type": "string" },
                  "version": { "type": "string" },
                  "capabilities": { "type": "object" }
                }
              }
            }
          }
        },
        "responses": { "200": { "description": "Relay registered or updated" } }
      }
    },
    "/api/auth/login": {
      "post": {
        "tags": ["auth"],
        "summary": "Password-based login",
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["email", "password"],
                "properties": {
                  "email": { "type": "string", "format": "email" },
                  "password": { "type": "string" }
                }
              }
            }
          }
        },
        "responses": {
          "200": { "description": "Session established" },
          "401": { "description": "Invalid credentials" }
        }
      }
    },
    "/api/auth/register": {
      "post": {
        "tags": ["auth"],
        "summary": "Public registration (requires TRACE_ALLOW_PUBLIC_REGISTER)",
        "responses": {
          "201": { "description": "User created" },
          "403": { "description": "Public register disabled" }
        }
      }
    },
    "/api/auth/invite/accept": {
      "post": {
        "tags": ["auth"],
        "summary": "Accept organization invite"
      }
    },
    "/api/auth/logout": {
      "post": {
        "tags": ["auth"],
        "summary": "Logout and clear session"
      }
    },
    "/api/auth/signup": {
      "post": {
        "tags": ["auth"],
        "summary": "Self-service signup"
      }
    },
    "/api/auth/verify-email": {
      "post": {
        "tags": ["auth"],
        "summary": "Verify email address"
      }
    },
    "/api/auth/forgot-password": {
      "post": {
        "tags": ["auth"],
        "summary": "Request password reset"
      }
    },
    "/api/auth/reset-password": {
      "post": {
        "tags": ["auth"],
        "summary": "Reset password with token"
      }
    },
    "/api/auth/mfa/status": {
      "get": {
        "tags": ["auth"],
        "summary": "MFA enrollment status"
      }
    },
    "/api/auth/mfa/enroll": {
      "post": {
        "tags": ["auth"],
        "summary": "Enroll MFA"
      }
    },
    "/api/auth/mfa/verify": {
      "post": {
        "tags": ["auth"],
        "summary": "Verify MFA code"
      }
    },
    "/api/auth/mfa/disable": {
      "post": {
        "tags": ["auth"],
        "summary": "Disable MFA"
      }
    },
    "/api/auth/mfa/resend-code": {
      "post": {
        "tags": ["auth"],
        "summary": "Resend MFA verification code"
      }
    },
    "/api/auth/oidc/login": {
      "get": {
        "tags": ["auth"],
        "summary": "OIDC authorize redirect",
        "responses": {
          "302": { "description": "Redirects to IdP" },
          "404": { "description": "OIDC not configured" }
        }
      }
    },
    "/api/auth/oidc/callback": {
      "get": {
        "tags": ["auth"],
        "summary": "OIDC callback (requires existing membership)"
      }
    },
    "/api/me": {
      "get": {
        "tags": ["auth"],
        "summary": "Current authenticated user",
        "security": [{ "bearerAuth": [] }],
        "responses": {
          "200": {
            "description": "User info",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "id": { "type": "string", "format": "uuid" },
                    "email": { "type": "string" },
                    "name": { "type": "string" },
                    "role": { "type": "string" },
                    "organization_id": { "type": "string", "format": "uuid" }
                  }
                }
              }
            }
          }
        }
      }
    },
    "/api/me/orgs": {
      "get": {
        "tags": ["auth"],
        "summary": "Organizations the current user belongs to"
      }
    },
    "/api/auth/switch-org": {
      "post": {
        "tags": ["auth"],
        "summary": "Switch active organization"
      }
    },
    "/api/me/sessions": {
      "get": {
        "tags": ["auth"],
        "summary": "List active sessions"
      }
    },
    "/api/me/sessions/{id}": {
      "delete": {
        "tags": ["auth"],
        "summary": "Revoke a session"
      }
    },
    "/api/me/usage": {
      "get": {
        "tags": ["auth"],
        "summary": "Usage metrics for billing"
      }
    },
    "/api/overview": {
      "get": {
        "tags": ["issues"],
        "summary": "Project overview with trends and KPIs",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/query/events": {
      "get": {
        "tags": ["events"],
        "summary": "Event query DSL",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/search": {
      "get": {
        "tags": ["admin"],
        "summary": "Full-text search across all resources",
        "security": [{ "bearerAuth": [] }],
        "parameters": [
          { "name": "q", "in": "query", "required": true, "schema": { "type": "string" }, "description": "Search query" }
        ]
      }
    },
    "/api/oncall": {
      "get": { "tags": ["admin"], "summary": "List on-call schedules", "security": [{ "bearerAuth": [] }] },
      "post": { "tags": ["admin"], "summary": "Create on-call schedule", "security": [{ "bearerAuth": [] }] }
    },
    "/api/oncall/shifts": {
      "post": {
        "tags": ["admin"],
        "summary": "Add shift to on-call schedule",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/escalation": {
      "get": { "tags": ["admin"], "summary": "List escalation policies", "security": [{ "bearerAuth": [] }] },
      "post": { "tags": ["admin"], "summary": "Create escalation policy", "security": [{ "bearerAuth": [] }] }
    },
    "/api/issues": {
      "get": {
        "tags": ["issues"],
        "summary": "List issues with filtering and pagination",
        "security": [{ "bearerAuth": [] }],
        "parameters": [
          { "name": "limit", "in": "query", "schema": { "type": "integer", "default": 25 } },
          { "name": "offset", "in": "query", "schema": { "type": "integer", "default": 0 } },
          { "name": "status", "in": "query", "schema": { "type": "string" } },
          { "name": "severity", "in": "query", "schema": { "type": "string" } },
          { "name": "q", "in": "query", "schema": { "type": "string" } },
          { "name": "X-Project-ID", "in": "header", "schema": { "type": "string", "format": "uuid" }, "description": "Target project" }
        ]
      }
    },
    "/api/issues/{id}": {
      "get": {
        "tags": ["issues"],
        "summary": "Issue detail with events, comments, activity",
        "security": [{ "bearerAuth": [] }],
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string", "format": "uuid" } }]
      }
    },
    "/api/issues/{id}/status": {
      "post": {
        "tags": ["issues"],
        "summary": "Update issue status",
        "security": [{ "bearerAuth": [] }],
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["status"],
                "properties": {
                  "status": {
                    "type": "string",
                    "enum": ["open", "investigating", "resolved", "ignored", "archived"]
                  }
                }
              }
            }
          }
        }
      }
    },
    "/api/issues/{id}/comments": {
      "post": {
        "tags": ["issues"],
        "summary": "Add comment to issue",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/issues/{id}/assign": {
      "post": {
        "tags": ["issues"],
        "summary": "Assign issue to user",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/issues/{id}/merge": {
      "post": {
        "tags": ["issues"],
        "summary": "Merge issue into another",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/issues/{id}/split": {
      "post": {
        "tags": ["issues"],
        "summary": "Split events to new issue",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/issues/{id}/labels": {
      "post": {
        "tags": ["issues"],
        "summary": "Set labels on issue",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/issues/{id}/suspect-commits": {
      "get": {
        "tags": ["issues"],
        "summary": "List suspect commits for issue",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/issues/{id}/replay": {
      "get": {
        "tags": ["issues"],
        "summary": "Breadcrumb replay frames for issue",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/events": {
      "get": {
        "tags": ["events"],
        "summary": "List events with filtering",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/events/{id}": {
      "get": {
        "tags": ["events"],
        "summary": "Event detail with stack frames and analysis",
        "security": [{ "bearerAuth": [] }],
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string", "format": "uuid" } }]
      }
    },
    "/api/events/{id}/raw": {
      "get": {
        "tags": ["events"],
        "summary": "Download raw LEP payload",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/events/{id}/reprocess": {
      "post": {
        "tags": ["events"],
        "summary": "Reprocess event (re-run analyzer)",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/events/reprocess-stale": {
      "post": {
        "tags": ["events"],
        "summary": "Reprocess all events with stale analyzer versions",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/devices": {
      "get": {
        "tags": ["devices"],
        "summary": "List devices",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/devices/{id}": {
      "get": {
        "tags": ["devices"],
        "summary": "Device detail with recent events",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/devices/{id}/firmware-history": {
      "get": {
        "tags": ["devices"],
        "summary": "Firmware upgrade history",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/releases": {
      "get": {
        "tags": ["releases"],
        "summary": "List releases",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/releases/{id}": {
      "get": {
        "tags": ["releases"],
        "summary": "Release detail",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/releases/{id}/stats": {
      "get": {
        "tags": ["releases"],
        "summary": "Release stats (crash-free rate, sessions)",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/releases/{id}/commits": {
      "post": {
        "tags": ["releases"],
        "summary": "Link commits to release for suspect analysis",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/artifacts": {
      "get": {
        "tags": ["artifacts"],
        "summary": "List artifacts",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/artifacts/{id}/promote": {
      "post": {
        "tags": ["artifacts"],
        "summary": "Promote artifact from quarantine",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/organizations": {
      "get": { "tags": ["orgs"], "summary": "List organizations", "security": [{ "bearerAuth": [] }] },
      "post": { "tags": ["orgs"], "summary": "Create organization", "security": [{ "bearerAuth": [] }] },
      "patch": { "tags": ["orgs"], "summary": "Update current organization", "security": [{ "bearerAuth": [] }] }
    },
    "/api/organizations/members": {
      "get": { "tags": ["orgs"], "summary": "List members", "security": [{ "bearerAuth": [] }] },
      "post": { "tags": ["orgs"], "summary": "Invite member", "security": [{ "bearerAuth": [] }] },
      "patch": { "tags": ["orgs"], "summary": "Update member role", "security": [{ "bearerAuth": [] }] },
      "delete": { "tags": ["orgs"], "summary": "Remove member", "security": [{ "bearerAuth": [] }] }
    },
    "/api/projects": {
      "get": { "tags": ["projects"], "summary": "List projects", "security": [{ "bearerAuth": [] }] },
      "post": { "tags": ["projects"], "summary": "Create project", "security": [{ "bearerAuth": [] }] }
    },
    "/api/projects/{id}": {
      "patch": { "tags": ["projects"], "summary": "Update project", "security": [{ "bearerAuth": [] }] },
      "delete": { "tags": ["projects"], "summary": "Delete project", "security": [{ "bearerAuth": [] }] }
    },
    "/api/alerts": {
      "get": { "tags": ["alerts"], "summary": "List alert rules", "security": [{ "bearerAuth": [] }] },
      "post": { "tags": ["alerts"], "summary": "Create alert rule", "security": [{ "bearerAuth": [] }] }
    },
    "/api/channels": {
      "get": { "tags": ["alerts"], "summary": "List notification channels", "security": [{ "bearerAuth": [] }] },
      "post": { "tags": ["alerts"], "summary": "Create notification channel (slack|discord|webhook|email|github)", "security": [{ "bearerAuth": [] }] }
    },
    "/api/relays": {
      "get": { "tags": ["relay"], "summary": "List relays", "security": [{ "bearerAuth": [] }] }
    },
    "/api/hardware": {
      "get": { "tags": ["devices"], "summary": "List hardware revisions", "security": [{ "bearerAuth": [] }] },
      "post": { "tags": ["devices"], "summary": "Create hardware revision", "security": [{ "bearerAuth": [] }] }
    },
    "/api/hardware/compare": {
      "get": { "tags": ["devices"], "summary": "Failures by hardware revision", "security": [{ "bearerAuth": [] }] }
    },
    "/api/audit": {
      "get": {
        "tags": ["admin"],
        "summary": "Audit log (admin-only)",
        "security": [{ "bearerAuth": [] }],
        "parameters": [
          { "name": "export", "in": "query", "schema": { "type": "boolean" }, "description": "Return as downloadable NDJSON" }
        ]
      }
    },
    "/api/tokens": {
      "get": { "tags": ["admin"], "summary": "List project tokens", "security": [{ "bearerAuth": [] }] },
      "post": { "tags": ["admin"], "summary": "Create project token", "security": [{ "bearerAuth": [] }] }
    },
    "/api/tokens/{id}": {
      "delete": {
        "tags": ["admin"],
        "summary": "Revoke token",
        "security": [{ "bearerAuth": [] }],
        "parameters": [{ "name": "id", "in": "path", "required": true, "schema": { "type": "string", "format": "uuid" } }]
      }
    },
    "/api/analytics/export": {
      "post": {
        "tags": ["analytics"],
        "summary": "Export recent events to analytics sink (NDJSON)",
        "security": [{ "bearerAuth": [] }],
        "parameters": [
          { "name": "hours", "in": "query", "schema": { "type": "integer", "default": 24 }, "description": "Hours to export" }
        ],
        "responses": {
          "200": {
            "description": "Export result",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "object_key": { "type": "string" },
                    "rows": { "type": "integer" },
                    "sink": { "type": "string", "enum": ["file", "objects"] }
                  }
                }
              }
            }
          }
        }
      }
    },
    "/api/bootstrap": {
      "get": {
        "tags": ["health"],
        "summary": "Bootstrap info (bootstrapped status, project)",
        "responses": {
          "200": {
            "description": "Bootstrap status",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "bootstrapped": { "type": "boolean" },
                    "open_ui": { "type": "boolean" },
                    "project": { "type": "object" }
                  }
                }
              }
            }
          }
        }
      }
    },
    "/api/settings": {
      "get": { "tags": ["admin"], "summary": "Get project settings", "security": [{ "bearerAuth": [] }] },
      "put": { "tags": ["admin"], "summary": "Update project settings (retention, analyzer version)", "security": [{ "bearerAuth": [] }] }
    },
    "/api/webhooks/deliveries": {
      "get": {
        "tags": ["alerts"],
        "summary": "List webhook delivery history",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/jobs/dead": {
      "get": {
        "tags": ["admin"],
        "summary": "List dead jobs (admin-only)",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/jobs/dead/{id}/requeue": {
      "post": {
        "tags": ["admin"],
        "summary": "Requeue dead job",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/labels": {
      "get": { "tags": ["admin"], "summary": "List labels", "security": [{ "bearerAuth": [] }] },
      "post": { "tags": ["admin"], "summary": "Create label", "security": [{ "bearerAuth": [] }] }
    },
    "/api/releases/compare": {
      "get": {
        "tags": ["releases"],
        "summary": "Compare releases (crash-free rates)",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/boots": {
      "get": {
        "tags": ["devices"],
        "summary": "Boot sessions",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/alerts/skips": {
      "get": {
        "tags": ["alerts"],
        "summary": "Alert skip list (admin-only)",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/scim/v2/Users": {
      "get": {
        "tags": ["admin"],
        "summary": "SCIM v2 User list (filter=userName eq, startIndex, count)",
        "security": [{ "bearerAuth": [] }]
      },
      "post": {
        "tags": ["admin"],
        "summary": "SCIM v2 User provision (creates passwordless user + membership)",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/scim/v2/Users/{id}": {
      "get": {
        "tags": ["admin"],
        "summary": "SCIM v2 User read",
        "security": [{ "bearerAuth": [] }]
      },
      "put": {
        "tags": ["admin"],
        "summary": "SCIM v2 User update (name, role; active=false deprovisions)",
        "security": [{ "bearerAuth": [] }]
      },
      "patch": {
        "tags": ["admin"],
        "summary": "SCIM v2 User patch (same as PUT)",
        "security": [{ "bearerAuth": [] }]
      },
      "delete": {
        "tags": ["admin"],
        "summary": "SCIM v2 User deprovision (removes membership)",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/scim/v2/Groups": {
      "get": {
        "tags": ["admin"],
        "summary": "SCIM v2 Group list (filter=displayName eq, startIndex, count)",
        "security": [{ "bearerAuth": [] }]
      },
      "post": {
        "tags": ["admin"],
        "summary": "SCIM v2 Group create (displayName + optional members[])",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/scim/v2/Groups/{id}": {
      "get": {
        "tags": ["admin"],
        "summary": "SCIM v2 Group read",
        "security": [{ "bearerAuth": [] }]
      },
      "put": {
        "tags": ["admin"],
        "summary": "SCIM v2 Group replace members (displayName immutable)",
        "security": [{ "bearerAuth": [] }]
      },
      "patch": {
        "tags": ["admin"],
        "summary": "SCIM v2 Group patch members (same as PUT)",
        "security": [{ "bearerAuth": [] }]
      },
      "delete": {
        "tags": ["admin"],
        "summary": "SCIM v2 Group delete (cascade members)",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/saml/config": {
      "get": {
        "tags": ["admin"],
        "summary": "Get SAML config (admin-only)",
        "security": [{ "bearerAuth": [] }]
      },
      "put": {
        "tags": ["admin"],
        "summary": "Update SAML config (admin-only)",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/saml/metadata": {
      "get": {
        "tags": ["auth"],
        "summary": "SAML SP metadata XML"
      }
    },
    "/saml/login": {
      "get": {
        "tags": ["auth"],
        "summary": "SAML IdP redirect",
        "responses": {
          "302": { "description": "Redirects to IdP SSO" },
          "400": { "description": "SAML not configured" }
        }
      }
    },
    "/saml/acs": {
      "post": {
        "tags": ["auth"],
        "summary": "SAML Assertion Consumer Service",
        "responses": {
          "200": { "description": "SAML login successful" },
          "400": { "description": "Invalid SAML assertion" },
          "501": { "description": "SAML not ready (signature verification required)" }
        }
      }
    },
    "/v1/admin/organizations/{id}/entitlements": {
      "post": {
        "tags": ["admin"],
        "summary": "Billing service: apply entitlements (HMAC-signed)",
        "security": [{ "billingHMAC": [] }],
        "parameters": [
          { "name": "id", "in": "path", "required": true, "schema": { "type": "string", "format": "uuid" }, "description": "Organization ID" }
        ],
        "description": "HMAC-signed endpoint called by the proprietary billing service. Requires TRACE_BILLING_HMAC_SECRET."
      },
      "get": {
        "tags": ["admin"],
        "summary": "Billing service: list entitlements (HMAC-signed)",
        "security": [{ "billingHMAC": [] }],
        "description": "HMAC-signed endpoint called by the proprietary billing service."
      }
    },
    "/api/admin/audit/verify": {
      "get": {
        "tags": ["admin"],
        "summary": "Audit chain integrity verification (admin-only)",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/org-tokens": {
      "post": {
        "tags": ["admin"],
        "summary": "Create organization token",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/webhooks/export": {
      "post": {
        "tags": ["analytics"],
        "summary": "Create analytics export job",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/webhooks/exports": {
      "get": {
        "tags": ["analytics"],
        "summary": "List analytics exports",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/webhooks/export/{id}": {
      "get": {
        "tags": ["analytics"],
        "summary": "Get analytics export status",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/onboarding/status": {
      "get": {
        "tags": ["auth"],
        "summary": "Onboarding progress",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/onboarding/step": {
      "post": {
        "tags": ["auth"],
        "summary": "Complete onboarding step",
        "security": [{ "bearerAuth": [] }]
      }
    },
    "/api/onboarding/progress": {
      "get": {
        "tags": ["auth"],
        "summary": "Get onboarding progress",
        "security": [{ "bearerAuth": [] }]
      }
    }
  },
  "components": {
    "securitySchemes": {
      "bearerAuth": {
        "type": "http",
        "scheme": "bearer",
        "bearerFormat": "token",
        "description": "Project token or session secret"
      },
      "billingHMAC": {
        "type": "http",
        "scheme": "bearer",
        "description": "HMAC-signed Admin API (billing service only)"
      }
    },
    "parameters": {
      "ProjectHeader": {
        "name": "X-Project-ID",
        "in": "header",
        "required": false,
        "schema": { "type": "string", "format": "uuid" },
        "description": "Target project (must belong to user organization)"
      }
    },
    "schemas": {
      "Error": {
        "type": "object",
        "properties": {
          "error": {
            "type": "object",
            "properties": {
              "code": { "type": "string" },
              "message": { "type": "string" },
              "retryable": { "type": "boolean" },
              "request_id": { "type": "string" }
            }
          }
        }
      },
      "Issue": {
        "type": "object",
        "properties": {
          "id": { "type": "string", "format": "uuid" },
          "title": { "type": "string" },
          "severity": { "type": "string", "enum": ["fatal", "error", "warning", "info"] },
          "status": { "type": "string", "enum": ["open", "investigating", "resolved", "ignored", "archived"] },
          "event_count": { "type": "integer" },
          "affected_devices": { "type": "integer" },
          "first_seen": { "type": "string", "format": "date-time" },
          "last_seen": { "type": "string", "format": "date-time" },
          "fingerprint": { "type": "string" },
          "assignee": { "type": "string" },
          "regression_count": { "type": "integer" },
          "probable_cause": { "type": "string" }
        }
      },
      "Event": {
        "type": "object",
        "properties": {
          "id": { "type": "string", "format": "uuid" },
          "event_id": { "type": "string" },
          "severity": { "type": "string" },
          "state": { "type": "string" },
          "pipeline": { "type": "string" },
          "received_at": { "type": "string", "format": "date-time" },
          "fingerprint": { "type": "string" },
          "architecture": { "type": "integer" },
          "frames": { "type": "array", "items": { "$ref": "#/components/schemas/StackFrame" } },
          "analysis": { "type": "object" }
        }
      },
      "StackFrame": {
        "type": "object",
        "properties": {
          "function": { "type": "string" },
          "file": { "type": "string" },
          "line": { "type": "integer" },
          "address": { "type": "integer" }
        }
      },
      "Device": {
        "type": "object",
        "properties": {
          "id": { "type": "string", "format": "uuid" },
          "device_id": { "type": "string" },
          "status": { "type": "string" },
          "product": { "type": "string" },
          "firmware_version": { "type": "string" },
          "build_id": { "type": "string" },
          "last_seen": { "type": "string", "format": "date-time" },
          "hardware_revision": { "type": "string" }
        }
      },
      "Release": {
        "type": "object",
        "properties": {
          "id": { "type": "string", "format": "uuid" },
          "version": { "type": "string" },
          "build_id": { "type": "string" },
          "git_commit": { "type": "string" },
          "status": { "type": "string" },
          "created_at": { "type": "string", "format": "date-time" }
        }
      }
    }
  },
  "security": [{ "bearerAuth": [] }]
}`
