import http from 'k6/http';
import { check } from 'k6';
import { Counter, Trend } from 'k6/metrics';

const corpus = JSON.parse(open('/corpus/corpus.json'));
const acceptedLatency = new Trend('accepted_latency', true);
const rejected = new Counter('rejected_requests');
const rate = Number(__ENV.RATE || 20);
const duration = __ENV.DURATION || '30s';

export const options = {
  scenarios: {
    ingest: {
      executor: 'constant-arrival-rate',
      rate,
      timeUnit: '1s',
      duration,
      preAllocatedVUs: Number(__ENV.PREALLOCATED_VUS || 20),
      maxVUs: Number(__ENV.MAX_VUS || 100),
    },
  },
  thresholds: { http_req_failed: ['rate==0'] },
};

export default function () {
  const sample = corpus[(__VU + __ITER) % corpus.length];
  const identity = `${__ENV.RUN_ID}-${__VU}-${__ITER}`;
  const body = JSON.stringify({
    tenant_code: 'acme',
    source: `capacity-${__ENV.RUN_ID}`,
    event_type: 'order.timeout',
    event_id: identity,
    business_key: identity,
    occurred_at: new Date().toISOString(),
    payload: { amount: 6999, source_event_type: sample.source_event_type, source_payload: sample.payload },
  });
  const response = http.post(`${__ENV.BASE_URL}/api/v1/events`, body, {
    headers: { Authorization: `Bearer ${__ENV.API_TOKEN}`, 'Content-Type': 'application/json' },
    timeout: '30s',
  });
  acceptedLatency.add(response.timings.duration);
  if (!check(response, { accepted: (r) => r.status === 202 })) rejected.add(1);
}

export function handleSummary(data) {
  return { '/results/k6-summary.json': JSON.stringify(data, null, 2) };
}
