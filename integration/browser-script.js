import { browser } from 'k6/browser';
import { sleep } from 'k6';
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
    await page.goto('https://www.amazon.com');
    sleep(2); // FIXME: Trying if this helps fix missing metrics.
  } finally {
    await page.close();
  }
}
