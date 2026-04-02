resource "conga_agent" "aaron" {
  name = "aaron"
  type = "user"

  depends_on = [conga_environment.this]
}

resource "conga_agent" "engineering" {
  name         = "engineering"
  type         = "team"
  gateway_port = 18790

  depends_on = [conga_environment.this]
}
