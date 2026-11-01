-- Reference data every environment needs (run it in production too, after the migrations).
-- Videos cannot be published without a category (videos_published_requirements), and the
-- migrations create the table empty, so a fresh database must load this once.
-- Idempotent: safe to re-run; edit names/order freely, slugs are what the API exposes.
INSERT INTO categories (name, slug, description, sort_order) VALUES
  ('Entertainment', 'entertainment', 'General entertainment', 10),
  ('Music',         'music',         'Music and performances', 20),
  ('Gaming',        'gaming',        'Gaming content',         30),
  ('Shorts',        'shorts',        'Short-form videos',      40),
  ('Documentary',   'documentary',   'Documentary films',      50),
  ('Comedy',        'comedy',        'Comedy',                 60),
  ('Education',     'education',     'Educational content',    70),
  ('Other',         'other',         'Everything else',        99)
ON CONFLICT (slug) DO NOTHING;
