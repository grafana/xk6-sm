import { check } from 'k6';
import http from 'k6/http';
import { Trend, Counter, Gauge } from 'k6/metrics';

const testHost = __ENV.TEST_HOST ? __ENV.TEST_HOST : "test-api.k6.io";

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

  http.get(`http://${testHost}`); // non-https.
  http.get(`https://${testHost}/public/crocodiles/`);
  http.get(`https://${testHost}/public/crocodiles2/`); // 404
  http.get(`https://${testHost}/public/crocodiles3/`, {
    tags: {
      // Used by multihttp scripts to store the user-defiend URL before interpolation.
      '__raw_url__': 'foobar',
    }
  }); // 404
  http.get(`https://${testHost}/public/crocodiles4/`); // 404
  http.get(`https://${testHost}/public/crocodiles4/`); // Second 404, to assert differences between failure rate and counter.
  http.get(`http://fail.internal/public/crocodiles4/`); // failed
}
