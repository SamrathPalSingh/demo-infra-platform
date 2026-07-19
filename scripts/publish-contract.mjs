#!/usr/bin/env node
import { readFile } from "node:fs/promises";

function need(name) {
  if (!process.env[name]) throw new Error(`${name} is required.`);
  return process.env[name];
}

async function request(route, { method = "GET", body } = {}) {
  const response = await fetch(`https://api.github.com${route}`, {
    method,
    headers: {
      Accept: "application/vnd.github+json",
      Authorization: `Bearer ${need("REGISTRY_WRITE_TOKEN")}`,
      "Content-Type": "application/json",
      "User-Agent": "infra-contract-poc",
      "X-GitHub-Api-Version": "2022-11-28"
    },
    body: body ? JSON.stringify(body) : undefined
  });
  if (response.status === 404 && method === "GET") return null;
  if (!response.ok) throw new Error(`GitHub API failed (${response.status}): ${(await response.text()).slice(0, 500)}`);
  return response.json();
}

async function main() {
  const repository = need("REGISTRY_REPO");
  const target = "contracts/platform/shared-platform.json";
  const contract = JSON.parse(await readFile("platform-contract.json", "utf8"));
  contract.invariants_markdown = await readFile("platform-invariants.md", "utf8");
  contract.observed_state = {
    "infra/storage-class.yaml": await readFile("infra/storage-class.yaml", "utf8"),
    "infra/network-state.yaml": await readFile("infra/network-state.yaml", "utf8"),
    "terraform/main.tf": await readFile("terraform/main.tf", "utf8")
  };
  contract.source_revision = process.env.GITHUB_SHA || "local";
  const current = await request(`/repos/${repository}/contents/${target}`);
  await request(`/repos/${repository}/contents/${target}`, {
    method: "PUT",
    body: {
      message: `chore(contracts): publish platform ${contract.source_revision.slice(0, 7)}`,
      content: Buffer.from(`${JSON.stringify(contract, null, 2)}\n`).toString("base64"),
      sha: current?.sha,
      branch: process.env.REGISTRY_BRANCH || "main"
    }
  });
  console.log(`Published ${target} to ${repository}.`);
}

main().catch((error) => { console.error(error.message); process.exitCode = 1; });

