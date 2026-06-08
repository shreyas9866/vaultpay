import http from 'k6/http';
import { check, sleep } from 'k6';

// This is the battle plan
export const options = {
    stages: [
        { duration: '5s', target: 50 },  // Ramp up to 50 concurrent users over 5 seconds
        { duration: '10s', target: 50 }, // Hold the bombardment at 50 users for 10 seconds
        { duration: '5s', target: 0 },   // Ramp down to 0 gracefully
    ],
};

export default function () {
    const url = 'http://localhost:8080/charges';
    
    const payload = JSON.stringify({
        amount: 5000,
        currency: 'USD',
    });

    const params = {
        headers: {
            'Content-Type': 'application/json',
            // __VU is Virtual User ID, __ITER is Iteration count. This guarantees a unique key!
            'Idempotency-Key': `k6-load-${__VU}-${__ITER}`, 
        },
    };

    // Fire the missile
    const res = http.post(url, payload, params);

    // Check if the server survived (201 Created) or defended itself (429 Rate Limited)
    check(res, {
        'success or rate limited': (r) => r.status === 201 || r.status === 429,
    });

    sleep(0.1); // Wait 100ms before firing again
}