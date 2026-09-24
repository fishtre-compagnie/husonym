-- A job's updated_at moves on every change, as the other tables' do: most writes to a job left it
-- where it was, so a caller could not tell that a job had changed since it read it.
CREATE TRIGGER update_husonym_api_jobs_updated_at
  BEFORE UPDATE ON husonym_api.jobs
  FOR EACH ROW
  EXECUTE FUNCTION update_updated_at_column();
