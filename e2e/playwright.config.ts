import { defineConfig } from '@playwright/test';

const appPort = 18080;
const gitlabPort = 18929;

export default defineConfig({
  testDir: './tests',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 60_000,
  reporter: [['list']],
  use: {
    baseURL: `http://127.0.0.1:${appPort}`,
    channel: 'chrome',
    locale: 'en-US',
    trace: 'retain-on-failure',
  },
  webServer: [
    {
      command: `../backend/bin/fakegitlab -addr 127.0.0.1:${gitlabPort}`,
      url: `http://127.0.0.1:${gitlabPort}/api/v4/user`,
      ignoreHTTPSErrors: true,
      reuseExistingServer: false,
      timeout: 20_000,
    },
    {
      command: 'rm -rf .data && ../backend/bin/vibefluence -config vibefluence.yaml',
      url: `http://127.0.0.1:${appPort}/readyz`,
      env: {
        VIBEFLUENCE_ADDR: `127.0.0.1:${appPort}`,
        VIBEFLUENCE_GITLAB_TOKEN: 'dev-token',
      },
      reuseExistingServer: false,
      timeout: 20_000,
    },
  ],
});
