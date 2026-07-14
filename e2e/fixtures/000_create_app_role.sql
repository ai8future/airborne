DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'airborne_app') THEN
        CREATE ROLE airborne_app LOGIN PASSWORD 'airborne-app-e2e' NOSUPERUSER NOBYPASSRLS;
    END IF;
END $$;
