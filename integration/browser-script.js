import { browser } from 'k6/browser';
import { check } from 'https://jslib.k6.io/k6-utils/1.5.0/index.js';

export const options = {
  scenarios: {
    ui: {
      executor: 'shared-iterations',
      options: {
        browser: {
          type: 'chromium',
        },
      },
    },
  },
  thresholds: {
    checks: ['rate==1.0'],
  },
};

export default async function () {
  const context = await browser.newContext();
  const page = await context.newPage();

  try {
    // e-commerce site as a torture test for metric generation.
    const response = await page.goto('https://www.amazon.com', {
      waitUntil: 'networkidle',
    });
    // Add a check to ensure we got a response
    check(response, {
      'status is 200': (r) => r.status() === 200,
    });
  } finally {
    await page.close();
  }
}
