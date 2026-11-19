DROP TRIGGER IF EXISTS forget_pii_scan_results_on_connection_config ON husonym_api.connections;
DROP FUNCTION IF EXISTS husonym_api.forget_pii_scan_results();
DROP TABLE IF EXISTS husonym_api.pii_scan_results;
