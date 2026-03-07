-- Orphan cleanup for Sakila sample database.
-- Run as a before_fk hook: after PKs+indexes, before FK creation.
-- {{schema}} is replaced at runtime with the configured schema name.

-- NULL out dangling optional references
UPDATE {{schema}}.address SET city_id = NULL WHERE city_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM {{schema}}.city c WHERE c.city_id = {{schema}}.address.city_id);
UPDATE {{schema}}.customer SET store_id = NULL WHERE store_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM {{schema}}.store s WHERE s.store_id = {{schema}}.customer.store_id);

-- DELETE orphaned child rows
DELETE FROM {{schema}}.payment WHERE customer_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM {{schema}}.customer c WHERE c.customer_id = {{schema}}.payment.customer_id);
DELETE FROM {{schema}}.payment WHERE rental_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM {{schema}}.rental r WHERE r.rental_id = {{schema}}.payment.rental_id);
DELETE FROM {{schema}}.rental WHERE inventory_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM {{schema}}.inventory i WHERE i.inventory_id = {{schema}}.rental.inventory_id);
DELETE FROM {{schema}}.rental WHERE customer_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM {{schema}}.customer c WHERE c.customer_id = {{schema}}.rental.customer_id);
DELETE FROM {{schema}}.inventory WHERE film_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM {{schema}}.film f WHERE f.film_id = {{schema}}.inventory.film_id);
DELETE FROM {{schema}}.film_actor WHERE film_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM {{schema}}.film f WHERE f.film_id = {{schema}}.film_actor.film_id);
DELETE FROM {{schema}}.film_actor WHERE actor_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM {{schema}}.actor a WHERE a.actor_id = {{schema}}.film_actor.actor_id);
DELETE FROM {{schema}}.film_category WHERE film_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM {{schema}}.film f WHERE f.film_id = {{schema}}.film_category.film_id);
DELETE FROM {{schema}}.film_category WHERE category_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM {{schema}}.category c WHERE c.category_id = {{schema}}.film_category.category_id);
