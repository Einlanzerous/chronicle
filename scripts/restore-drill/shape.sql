-- CHRN-132 / CHRN-68: the SHAPE manifest. One SELECT, read-only, no literal
-- that names a mutating verb: restore-drill.sh refuses to send a file that
-- does. It renders a database's structure as sorted text lines so that
-- `diff live restored` is the whole comparison. Nothing here reads a row of
-- authored content, so the output is safe to leave the container as text.
--
-- Oids and attnums are left out on purpose: a restore renumbers both, and a
-- manifest that differed on them would differ on every drill.
--
-- An ACL is rendered as what it MEANS: a NULL acl is the object's default
-- (acldefault), because pg_dump leaves out an entry equal to the default and
-- so a live object whose owner once ran GRANT or REVOKE on it carries an
-- explicit copy of the default that its restored twin does not. Compared raw,
-- thirty tables differ on the first drill and nothing is wrong. The first
-- drill found this; every ACL line below goes through acldefault for that
-- reason.
SELECT l FROM (
  SELECT 'db|' || datname || '|owner=' || pg_get_userbyid(datdba)
         || '|acl=' || coalesce(datacl, acldefault('d', datdba))::text AS l
    FROM pg_database WHERE datname = current_database()
  UNION ALL
  SELECT 'schema|' || nspname || '|owner=' || pg_get_userbyid(nspowner)
         || '|acl=' || coalesce(nspacl, acldefault('n', nspowner))::text
    FROM pg_namespace
   WHERE nspname NOT LIKE 'pg\_%' AND nspname <> 'information_schema'
  UNION ALL
  SELECT 'rel|' || n.nspname || '.' || c.relname || '|kind=' || c.relkind::text
         || '|owner=' || pg_get_userbyid(c.relowner)
         || '|acl=' || coalesce(c.relacl, acldefault(CASE c.relkind WHEN 'S' THEN 's'::"char" ELSE 'r'::"char" END, c.relowner))::text
    FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
   WHERE n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'
     AND c.relkind IN ('r', 'p', 'v', 'm', 'S', 'f')
  UNION ALL
  SELECT 'col|' || n.nspname || '.' || c.relname || '.' || a.attname
         || '|' || format_type(a.atttypid, a.atttypmod)
         || '|notnull=' || a.attnotnull
         || '|default=' || coalesce(pg_get_expr(d.adbin, d.adrelid), '')
         || '|generated=' || a.attgenerated::text || '|identity=' || a.attidentity::text
    FROM pg_attribute a
    JOIN pg_class c ON c.oid = a.attrelid
    JOIN pg_namespace n ON n.oid = c.relnamespace
    LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
   WHERE a.attnum > 0 AND NOT a.attisdropped
     AND c.relkind IN ('r', 'p', 'v', 'm', 'f')
     AND n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'
  UNION ALL
  SELECT 'con|' || n.nspname || '.' || c.relname || '|' || k.conname
         || '|' || pg_get_constraintdef(k.oid)
    FROM pg_constraint k
    JOIN pg_class c ON c.oid = k.conrelid
    JOIN pg_namespace n ON n.oid = c.relnamespace
   WHERE n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'
  UNION ALL
  SELECT 'idx|' || schemaname || '.' || tablename || '|' || indexname
         || '|' || indexdef
    FROM pg_indexes
   WHERE schemaname NOT LIKE 'pg\_%' AND schemaname <> 'information_schema'
  UNION ALL
  SELECT 'trg|' || n.nspname || '.' || c.relname || '|' || t.tgname
         || '|enabled=' || t.tgenabled::text || '|' || pg_get_triggerdef(t.oid)
    FROM pg_trigger t
    JOIN pg_class c ON c.oid = t.tgrelid
    JOIN pg_namespace n ON n.oid = c.relnamespace
   WHERE NOT t.tgisinternal
  UNION ALL
  SELECT 'fn|' || n.nspname || '.' || p.proname || '(' || pg_get_function_identity_arguments(p.oid) || ')'
         || '|owner=' || pg_get_userbyid(p.proowner)
         || '|acl=' || coalesce(p.proacl, acldefault('f', p.proowner))::text
         || '|md5=' || md5(pg_get_functiondef(p.oid))
    FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
   WHERE n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'
     AND p.prokind IN ('f', 'p')
     AND NOT EXISTS (SELECT 1 FROM pg_depend e WHERE e.objid = p.oid AND e.deptype = 'e')
  UNION ALL
  SELECT 'defacl|' || pg_get_userbyid(defaclrole) || '|'
         || coalesce((SELECT nspname FROM pg_namespace WHERE oid = defaclnamespace), '')
         || '|' || defaclobjtype::text || '|' || defaclacl::text
    FROM pg_default_acl
  UNION ALL
  SELECT 'ext|' || extname || '|' || extversion FROM pg_extension
  UNION ALL
  SELECT 'role|' || rolname || '|super=' || rolsuper || '|inherit=' || rolinherit
         || '|createrole=' || rolcreaterole || '|createdb=' || rolcreatedb
         || '|login=' || rolcanlogin || '|repl=' || rolreplication
         || '|bypassrls=' || rolbypassrls || '|connlimit=' || rolconnlimit
    FROM pg_roles WHERE rolname IN ('chronicle', 'chronicle_tier1', 'asr')
  UNION ALL
  SELECT 'member|' || pg_get_userbyid(m.roleid) || '|' || pg_get_userbyid(m.member)
         || '|admin=' || m.admin_option
    FROM pg_auth_members m
   WHERE pg_get_userbyid(m.roleid) IN ('chronicle', 'chronicle_tier1', 'asr')
      OR pg_get_userbyid(m.member) IN ('chronicle', 'chronicle_tier1', 'asr')
  UNION ALL
  SELECT 'dbset|' || coalesce((SELECT datname FROM pg_database WHERE oid = s.setdatabase), '')
         || '|' || coalesce(pg_get_userbyid(nullif(s.setrole, 0)), '')
         || '|' || s.setconfig::text
    FROM pg_db_role_setting s
   WHERE s.setdatabase = (SELECT oid FROM pg_database WHERE datname = current_database())
      OR pg_get_userbyid(nullif(s.setrole, 0)) IN ('chronicle', 'chronicle_tier1', 'asr')
) q
ORDER BY l COLLATE "C";
