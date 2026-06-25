---
title: Guides
description: Source and destination guides for migrating MySQL, MariaDB, SQLite, and MSSQL to PostgreSQL on Supabase, Neon, Railway, Render, and more.
---

Guides for specific sources, destinations, and the combinations in between — the caveats, the defaults, and a sensible place to start for each one.

## By source database

- [MySQL to PostgreSQL](/guides/mysql/)
- [MariaDB to PostgreSQL](/guides/mariadb/)
- [SQLite to PostgreSQL](/guides/sqlite/)
- [MSSQL to PostgreSQL](/guides/mssql/)

## By managed Postgres destination

Destination-specific playbooks: the real provider connection, TLS, pooling, and firewall setup — not just generic source advice rewarmed. Grouped by where your data is headed:

- **Supabase**: [from MySQL](/guides/mysql-to-supabase/) · [from MSSQL](/guides/mssql-to-supabase/) · [from Azure SQL](/guides/azure-sql-to-supabase/) · [from PlanetScale](/guides/planetscale-to-supabase/) · [from RDS MySQL](/guides/aws-rds-mysql-to-supabase/) · [from Cloud SQL MySQL](/guides/cloud-sql-mysql-to-supabase/)
- **Neon**: [from MySQL](/guides/mysql-to-neon/) · [from MSSQL](/guides/mssql-to-neon/) · [from Azure SQL](/guides/azure-sql-to-neon/) · [from PlanetScale](/guides/planetscale-to-neon/) · [from RDS MySQL](/guides/aws-rds-mysql-to-neon/) · [from Cloud SQL MySQL](/guides/cloud-sql-mysql-to-neon/)
- **PlanetScale Postgres**: [from MySQL](/guides/mysql-to-planetscale-postgres/) · [from MSSQL](/guides/mssql-to-planetscale-postgres/)
- **Railway Postgres**: [from MySQL](/guides/mysql-to-railway-postgres/) · [from MSSQL](/guides/mssql-to-railway-postgres/)
- **Render Postgres**: [from MySQL](/guides/mysql-to-render-postgres/) · [from MSSQL](/guides/mssql-to-render-postgres/)

## By managed source environment

Where your source lives changes the access, TLS, and firewall story as much as the destination does. These guides cover the source-side specifics for managed and hosted databases:

- **Managed MySQL sources**: PlanetScale (MySQL/Vitess) — [to Supabase](/guides/planetscale-to-supabase/) · [to Neon](/guides/planetscale-to-neon/); AWS RDS / Aurora MySQL — [to Supabase](/guides/aws-rds-mysql-to-supabase/) · [to Neon](/guides/aws-rds-mysql-to-neon/); Google Cloud SQL for MySQL — [to Supabase](/guides/cloud-sql-mysql-to-supabase/) · [to Neon](/guides/cloud-sql-mysql-to-neon/)
- **Azure SQL / SQL Server sources**: [Azure SQL to Supabase](/guides/azure-sql-to-supabase/) · [Azure SQL to Neon](/guides/azure-sql-to-neon/) · [MSSQL to Railway](/guides/mssql-to-railway-postgres/) · [MSSQL to Render](/guides/mssql-to-render-postgres/)
