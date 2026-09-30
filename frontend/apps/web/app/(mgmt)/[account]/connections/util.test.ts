import { buildConnectionConfigGcpCloudStorage } from './util';

// The key of the service account goes to the connection; an empty field sends none, which the
// API refuses unless the deployment lets connections act with the servers' identity.
describe('buildConnectionConfigGcpCloudStorage', () => {
  it('sends the service account key', () => {
    const config = buildConnectionConfigGcpCloudStorage({
      connectionName: 'gcs',
      gcp: {
        bucket: 'the-bucket',
        pathPrefix: '',
        serviceAccountCredentials: '{"type": "service_account"}',
      },
    });
    expect(config.config.case).toBe('gcpCloudstorageConfig');
    expect(config.config.value).toMatchObject({
      bucket: 'the-bucket',
      serviceAccountCredentials: '{"type": "service_account"}',
    });
  });

  it('sends no key for an empty field', () => {
    const config = buildConnectionConfigGcpCloudStorage({
      connectionName: 'gcs',
      gcp: { bucket: 'the-bucket', serviceAccountCredentials: '' },
    });
    expect(config.config.case).toBe('gcpCloudstorageConfig');
    if (config.config.case === 'gcpCloudstorageConfig') {
      expect(config.config.value.serviceAccountCredentials).toBeUndefined();
    }
  });
});
