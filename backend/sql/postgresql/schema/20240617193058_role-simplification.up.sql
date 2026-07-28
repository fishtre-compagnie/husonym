DO $$
BEGIN
    -- Step 1: Reassign ownership of objects from specific roles to the current user
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'husonym_api_owner') THEN
        REASSIGN OWNED BY husonym_api_owner TO current_user;
    END IF;
END
$$;
