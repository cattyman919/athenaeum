-- docs/02-data-model.md §6
CREATE OR REPLACE VIEW book_locations AS
SELECT
  bc.id                  AS copy_id,
  bc.barcode,
  bc.status              AS copy_status,
  b.id                   AS book_id,
  b.title,
  bd.code                AS building_code,
  bd.name                AS building_name,
  fl.floor_number,
  fl.name                AS floor_name,
  se.code                AS section_code,
  se.name                AS section_name,
  se.color               AS section_color,
  ai.code                AS aisle_code,
  sh.id                  AS shelf_id,
  sh.code                AS shelf_code,
  sh.row_level,
  sh.height_tier,
  bd.code || '/' || fl.floor_number::text || 'F/' || se.code || '/' ||
  ai.code || '/' || sh.code                AS path,          -- MAIN/3F/SF/A-1/S-2
  NULLIF(bd.name, '') || ' (' || fl.name || ')' || ' → ' || se.name
  || ' → ' || ai.code || ' → ' || sh.code || ' (' || sh.height_tier || ')'
                                             AS display_ladder
FROM book_copies bc
JOIN books b      ON b.id = bc.book_id
LEFT JOIN shelves sh   ON sh.id = bc.shelf_id
LEFT JOIN aisles ai    ON ai.id = sh.aisle_id
LEFT JOIN sections se  ON se.id = ai.section_id
LEFT JOIN floors fl    ON fl.id = se.floor_id
LEFT JOIN buildings bd ON bd.id = fl.building_id;
