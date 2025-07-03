import { sleep } from 'k6';
import { browser, devices } from 'k6/browser';

export const options = {
    scenarios: {
        browser: {
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

const sessionId = "session-" + Math.random().toString(36).substr(2, 9);

export default async function () {
    const context = await browser.newContext();
    await context.addInitScript(`
        const server = "http://localhost:23456";
        function initRRWeb() {
            if (!window.rrweb) {
                const script = document.createElement('script');
                script.src = "https://cdn.jsdelivr.net/npm/rrweb@2.0.0-alpha.4/dist/rrweb.min.js";
                script.onload = () => {
                    window.rrweb.record({
                        emit(event) {
                            fetch(server + "/rrweb/", {
                                method: "POST",
                                headers: { "Content-Type": "application/json" },
                                body: JSON.stringify({ session_id: "${sessionId}", event }),
                            }).catch(console.error);
                        }
                    });
                };
                document.head.appendChild(script);
            }
        }

        if (document.readyState === 'loading') {
            document.addEventListener('DOMContentLoaded', initRRWeb);
        } else {
            initRRWeb();
        }
    `);
    const page = await context.newPage();

    await page.goto("https://grafana.com/");
    sleep(2);

    await page
      .locator('.align-items-center > [data-dropdown="products"]')
      .click();
    sleep(2);

    await page.locator("div:nth-of-type(4) a:nth-of-type(1) > div .copy").click();
    sleep(2);

    await page.locator("div:nth-of-type(5) .flex-direction-column > div").click();
    sleep(2);

    await page
      .locator(
        "html > body:nth-of-type(1) > div:nth-of-type(2) > div:nth-of-type(1) > div:nth-of-type(5) > div:nth-of-type(1) > div:nth-of-type(1) > div:nth-of-type(1) > div:nth-of-type(1) > div:nth-of-type(1) > div:nth-of-type(1) > ul:nth-of-type(1) > li:nth-of-type(1) > a:nth-of-type(1)",
      )
      .click();
    sleep(2);

    await page.locator("section:nth-of-type(1) .expand-table-btn").click();
    sleep(2);

    await page.locator(".table-modal").click();

    sleep(2);
    sleep(60);

    await page.close();

}
