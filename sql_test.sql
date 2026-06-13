-- === Schema ===

CREATE TABLE organizations (name TEXT, plan TEXT, seats INT);
CREATE TABLE subscriptions (org_id INT, status TEXT, amount BIGINT, interval TEXT);
CREATE TABLE invoices (sub_id INT, amount BIGINT, paid BOOLEAN, number TEXT);
CREATE TABLE api_keys (org_id INT, key TEXT, active BOOLEAN);
CREATE TABLE feature_flags (name TEXT, enabled BOOLEAN, rollout_pct INT);

-- === Seed data ===

INSERT INTO organizations (name, plan, seats) VALUES ('acme-corp', 'enterprise', 120);
INSERT INTO organizations (name, plan, seats) VALUES ('startup-io', 'starter', 5);
INSERT INTO organizations (name, plan, seats) VALUES ('bigbank', 'enterprise', 500);
INSERT INTO organizations (name, plan, seats) VALUES ('devshop', 'pro', 32);

INSERT INTO subscriptions (org_id, status, amount, interval) VALUES (1, 'active', 49900, 'monthly');
INSERT INTO subscriptions (org_id, status, amount, interval) VALUES (2, 'active', 2900, 'monthly');
INSERT INTO subscriptions (org_id, status, amount, interval) VALUES (3, 'active', 99900, 'annual');
INSERT INTO subscriptions (org_id, status, amount, interval) VALUES (4, 'trialing', 7900, 'monthly');
INSERT INTO subscriptions (org_id, status, amount, interval) VALUES (1, 'cancelled', 49900, 'monthly');

INSERT INTO invoices (sub_id, amount, paid, number) VALUES (1, 49900, true, 'INV-2026-0001');
INSERT INTO invoices (sub_id, amount, paid, number) VALUES (1, 49900, true, 'INV-2026-0002');
INSERT INTO invoices (sub_id, amount, paid, number) VALUES (2, 2900, true, 'INV-2026-0003');
INSERT INTO invoices (sub_id, amount, paid, number) VALUES (2, 2900, false, 'INV-2026-0004');
INSERT INTO invoices (sub_id, amount, paid, number) VALUES (3, 99900, true, 'INV-2026-0005');
INSERT INTO invoices (sub_id, amount, paid, number) VALUES (4, 7900, false, 'INV-2026-0006');

INSERT INTO api_keys (org_id, key, active) VALUES (1, 'sk_live_acme_abc123', true);
INSERT INTO api_keys (org_id, key, active) VALUES (1, 'sk_live_acme_def456', true);
INSERT INTO api_keys (org_id, key, active) VALUES (2, 'sk_live_sio_ghi789', true);
INSERT INTO api_keys (org_id, key, active) VALUES (3, 'sk_live_bb_jkl012', false);

INSERT INTO feature_flags (name, enabled, rollout_pct) VALUES ('dark_mode', true, 100);
INSERT INTO feature_flags (name, enabled, rollout_pct) VALUES ('ai_copilot', true, 25);
INSERT INTO feature_flags (name, enabled, rollout_pct) VALUES ('export_csv', false, 0);

-- === Queries ===

-- Which orgs are on enterprise plan?
SELECT name FROM organizations WHERE plan = 'enterprise';

-- Who has more than 50 seats?
SELECT name, seats FROM organizations WHERE seats > 50;

-- Active monthly subscriptions
SELECT org_id, amount FROM subscriptions WHERE status = 'active' AND interval = 'monthly';

-- Unpaid invoices
SELECT number, amount FROM invoices WHERE paid = false;

-- High-value paid invoices
SELECT number, amount FROM invoices WHERE amount > 50000 AND paid = true;

-- Deactivate a leaked API key
DELETE FROM api_keys WHERE key = 'sk_live_acme_def456';

-- Check remaining keys for org 1
SELECT key, active FROM api_keys WHERE org_id = 1;

-- Cancel devshop trial
DELETE FROM subscriptions WHERE org_id = 4 AND status = 'trialing';

-- Verify
SELECT org_id, status FROM subscriptions WHERE org_id = 4;
