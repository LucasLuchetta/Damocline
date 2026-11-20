import http from 'k6/http';
import { sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const vus = Number(__ENV.LAB_VUS);
if (![1, 4, 20].includes(vus)) throw new Error('LAB_VUS must be 1, 4, or 20');
const failed = new Rate('work_failures');
const latency = new Trend('work_duration', true);
export const options = {
  scenarios: { work: { executor: 'constant-vus', vus, duration: '20s', gracefulStop: '3s' } },
  maxRedirects: 0,
  rps: 100,
  noConnectionReuse: false,
  noVUConnectionReuse: false,
  summaryTrendStats: ['count', 'p(50)', 'p(95)', 'p(99)'],
};

export default function () {
  for (let step = 0; step < 2; step++) {
    const start = Date.now();
    const response = http.get('http://127.0.0.1:18086/work', { timeout: '3s' });
    latency.add(Date.now() - start);
    const success = response.status === 200 && (response.body ?? '').includes('synthetic work complete');
    failed.add(!success);
    if (step === 0) sleep(1);
  }
}
