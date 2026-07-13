"use strict";

/**
 * PM2 ecosystem: all secrets and host-specific values come from the environment.
 *
 * Load env then start (from any cwd):
 *   set -a && source /path/to/PlatariumGatewayGO/.env.gateway && set +a
 *   pm2 start /path/to/PlatariumGatewayGO/ecosystem.gateway.config.js
 *
 * Or: env $(grep -v '^#' .env.gateway | xargs) pm2 start ecosystem.gateway.config.js
 */

const path = require("path");

function env(name, defaultValue) {
  const v = process.env[name];
  if (v == null || String(v).trim() === "") return defaultValue;
  return String(v).trim();
}

function requireEnv(name) {
  const v = env(name, null);
  if (v == null) {
    throw new Error(`[ecosystem.gateway.config.js] Missing required env: ${name} (see .env.gateway.example)`);
  }
  return v;
}

const gatewayDir = requireEnv("PLATARIUM_GATEWAY_CWD");
const nodeHost = requireEnv("NODE_HOST");
const iceJson = env(
  "WEBRTC_ICE_SERVERS_JSON",
  '[{"urls":"stun:stun.l.google.com:19302"},{"urls":"stun:stun1.l.google.com:19302"}]',
);
const cliPath = env(
  "PLATARIUM_CLI_PATH",
  path.join(__dirname, "..", "PlatariumCore", "target", "release", "platarium-cli"),
);

const commonEnv = {
  NODE_HOST: nodeHost,
  PLATARIUM_CLI_PATH: cliPath,
  WEBRTC_ICE_SERVERS_JSON: iceJson,
};

module.exports = {
  apps: [
    {
      name: "PlatariumGatewayGO01",
      cwd: gatewayDir,
      script: "./platarium-gateway",
      args: [
        "--testnet",
        "--port",
        env("GATEWAY01_REST_PORT", "1812"),
        "--ws",
        env("GATEWAY01_WS_PORT", "1813"),
      ],
      env: { ...commonEnv },
    },
    {
      name: "PlatariumGatewayGO02",
      cwd: gatewayDir,
      script: "./platarium-gateway",
      args: [
        "--testnet",
        "--port",
        env("GATEWAY02_REST_PORT", "1822"),
        "--ws",
        env("GATEWAY02_WS_PORT", "1823"),
      ],
      env: { ...commonEnv },
    },
  ],
};
