-- Post-migration tasks for Sakila sample database.
-- Run as an after_all hook: after FKs, sequences, and triggers are in place.

-- Create a convenience view joining film + language
CREATE OR REPLACE VIEW {{schema}}.film_list AS
SELECT
    f.film_id,
    f.title,
    f.description,
    f.release_year,
    l.name AS language,
    f.rental_duration,
    f.rental_rate,
    f.length,
    f.replacement_cost,
    f.rating
FROM {{schema}}.film f
LEFT JOIN {{schema}}.language l ON l.language_id = f.language_id;

-- Analyze all tables so the planner has fresh stats
ANALYZE {{schema}}.actor;
ANALYZE {{schema}}.address;
ANALYZE {{schema}}.category;
ANALYZE {{schema}}.city;
ANALYZE {{schema}}.country;
ANALYZE {{schema}}.customer;
ANALYZE {{schema}}.film;
ANALYZE {{schema}}.film_actor;
ANALYZE {{schema}}.film_category;
ANALYZE {{schema}}.inventory;
ANALYZE {{schema}}.language;
ANALYZE {{schema}}.payment;
ANALYZE {{schema}}.rental;
ANALYZE {{schema}}.staff;
ANALYZE {{schema}}.store;
