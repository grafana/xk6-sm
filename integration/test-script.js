import { check } from 'k6';
import http from 'k6/http';
import { Trend, Counter, Gauge } from 'k6/metrics';

const testHost = __ENV.TEST_HOST ? __ENV.TEST_HOST : "quickpizza.grafana.com";

const myTrend = new Trend('waiting_time');
const myCounter = new Counter('my_counter');
const myGauge = new Gauge('my_gauge');

export const options = {
  iterations: 1,
};

export default function () {
  myTrend.add(0.5);
  myTrend.add(0.6);
  myTrend.add(0.7);

  myGauge.add(5);
  myGauge.add(6); // Discards previous value.

  myCounter.add(1);
  myCounter.add(2);

  check({}, {
      'something': () => true,
    }
  );
  check({}, {
      'something': () => false,
    }
  );
  check({}, {
      'something': () => false,
    }
  );

  http.get(`https://${testHost}/login`);
  http.get(`https://${testHost}/thats-a-404`); // 404
  http.get(`https://${testHost}/thats-another-404-accessed-twice`); // 404
  http.get(`https://${testHost}/thats-another-404-accessed-twice`); // Second 404, to assert differences between failure rate and counter.
  http.get(`https://${testHost}/404-with-raw-url-tag`, {
    tags: {
      // Used by multihttp scripts to store the user-defiend URL before interpolation.
      '__raw_url__': 'foobar',
    }
  }); // 404
  http.get(`http://fail.internal/failure-nxdomain`); // failed
}
