# Mittere API Documentation

Base URL: `http://{bind_ip}:{port}` (default: `http://127.0.0.1:9800`)

## Authentication

All endpoints require a Bearer token in the `Authorization` header:

```
Authorization: Bearer <token>
```

Tokens are validated against:
1. The static `auth_token` from config (matches as the `system` user)
2. User records in MongoDB (matched by `token` field)

## Response Format

All responses follow this structure:

```json
{
  "data": {},
  "success": true,
  "status_message": "Success",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

On error:

```json
{
  "success": false,
  "status_message": "Error description",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

---

## Telegram Notifications

### Send Notification

Send a message to Telegram subscribers.

```
POST /tg/msg
```

**Request body:**

| Field      | Type   | Required | Description                                                                 |
|------------|--------|----------|-----------------------------------------------------------------------------|
| `type`     | string | yes      | Event category (e.g. `"alert"`, `"notification"`, `"report"`)               |
| `subject`  | string | yes      | Event topic (e.g. `"deployment"`, `"monitoring"`)                           |
| `role`     | string | no       | Target role: `"user"` (default, all subscribers) or `"admin"` (admins only) |
| `username` | string | no       | If set, deliver only to this Telegram username                              |
| `text`     | string | no       | Message body                                                                |
| `payload`  | any    | no       | Arbitrary structured data (rendered as code block)                          |
| `time`     | string | no       | ISO 8601 timestamp                                                          |

**Routing rules:**
- No `role` or `role: "user"` — delivered to all active subscribers (admins + users)
- `role: "admin"` — delivered only to admin subscribers
- `username` set — delivered only to the subscriber with that Telegram username (overrides role filtering)

**Example:**

```bash
curl -X POST http://127.0.0.1:9800/tg/msg \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "alert",
    "subject": "deployment",
    "text": "Service v1.2.3 deployed to production",
    "payload": {"version": "1.2.3", "env": "prod"}
  }'
```

**Telegram message format:**

```
*alert*: `#deployment`
Service v1.2.3 deployed to production
```json
{"version":"1.2.3","env":"prod"}
```
```

**Response:** `200 OK`

```json
{
  "success": true,
  "status_message": "Success",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### Send Test Event

Sends a hardcoded test notification to all subscribers.

```
GET /tg/test
```

**Example:**

```bash
curl http://127.0.0.1:9800/tg/test \
  -H "Authorization: Bearer <token>"
```

---

## Users

Manage users. A user can be API-only (with a token for HTTP auth), Telegram-only (with a `user_id` for receiving messages), or both. All users are stored in the `subscriptions` MongoDB collection.

### User Model

| Field               | Type   | Description                                         |
|---------------------|--------|-----------------------------------------------------|
| `username`          | string | Unique identifier (required)                        |
| `user_id`           | int64  | Telegram user ID (0 if API-only)                    |
| `name`              | string | Display name                                        |
| `email`             | string | Email address                                       |
| `token`             | string | API authentication token (redacted in responses)    |
| `role`              | string | `"admin"` or `"user"`                               |
| `state`             | string | `"await"`, `"active"`, or `"disabled"`              |
| `is_verified`       | bool   | Whether invite code was confirmed                   |
| `subscription_type` | string | Subscription category (default: `"status"`)         |

### List Users

```
GET /users/
```

Returns all users. Token fields are redacted in the response.

**Example:**

```bash
curl http://127.0.0.1:9800/users/ \
  -H "Authorization: Bearer <token>"
```

**Response:** `200 OK`

```json
{
  "data": [
    {
      "username": "ci-bot",
      "name": "CI Bot",
      "email": "ci@example.com",
      "role": "user",
      "state": "active",
      "is_verified": true
    },
    {
      "user_id": 123456789,
      "username": "johndoe",
      "role": "admin",
      "state": "active",
      "is_verified": true,
      "subscription_type": "status"
    }
  ],
  "success": true,
  "status_message": "Success",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### Get User

```
GET /users/{username}
```

Returns a single user by username. Token is redacted.

**Example:**

```bash
curl http://127.0.0.1:9800/users/ci-bot \
  -H "Authorization: Bearer <token>"
```

**Response:** `200 OK` / `404 Not Found`

### Create User

```
POST /users/
```

**Request body:**

| Field      | Type   | Required | Description                          |
|------------|--------|----------|--------------------------------------|
| `username` | string | yes      | Unique username                      |
| `name`     | string | no       | Display name                         |
| `email`    | string | no       | Email address                        |
| `token`    | string | no       | API auth token (omit for TG-only)    |
| `role`     | string | no       | `"admin"` or `"user"` (default)      |
| `state`    | string | no       | `"active"`, `"await"`, `"disabled"`  |
| `user_id`  | int64  | no       | Telegram user ID (omit for API-only) |

**Example — API-only user:**

```bash
curl -X POST http://127.0.0.1:9800/users/ \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "ci-bot",
    "name": "CI Bot",
    "token": "secret-token-for-ci-bot",
    "role": "user",
    "state": "active"
  }'
```

**Response:** `201 Created`

### Update User

```
PUT /users/{username}
```

Updates an existing user. The `username` path parameter takes precedence over any username in the body.

**Example:**

```bash
curl -X PUT http://127.0.0.1:9800/users/ci-bot \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "CI Bot Updated",
    "role": "admin"
  }'
```

**Response:** `200 OK`

### Delete User

```
DELETE /users/{username}
```

**Example:**

```bash
curl -X DELETE http://127.0.0.1:9800/users/ci-bot \
  -H "Authorization: Bearer <token>"
```

**Response:** `200 OK`

---

## Error Codes

| Status | Meaning                                    |
|--------|--------------------------------------------|
| 200    | Success                                    |
| 201    | Created (user creation)                    |
| 400    | Bad request (invalid JSON or validation)   |
| 401    | Unauthorized (missing or invalid token)    |
| 404    | Not found                                  |
| 500    | Internal server error                      |
