-- +goose Up

-- El asistente ya puede confirmar un gasto recurrente.
--
-- Hasta aquí estaba ciego a ellos: si le decías "ya pagué el internet", lo
-- anotaba como un gasto suelto y la ocurrencia del recurrente se quedaba
-- esperando en el resumen. Después confirmabas esa también y el internet
-- quedaba registrado DOS VECES. Son dos caminos que escriben en la misma
-- tabla, y ninguno sabía del otro.
--
-- Con este tipo de propuesta el chat deja de crear un gasto paralelo: prepara
-- la MISMA ocurrencia que ya estaba pendiente, con el monto que dijo la
-- persona (el recibo nunca llega igual), y al confirmarla se resuelve el
-- recurrente. Un solo movimiento, venga del resumen o del chat.
ALTER TABLE agente_propuestas DROP CONSTRAINT agente_propuestas_tipo_valido;
ALTER TABLE agente_propuestas ADD CONSTRAINT agente_propuestas_tipo_valido
    CHECK (tipo IN ('movimiento', 'marcar_pagado', 'abono', 'recurrente'));

-- +goose Down
DELETE FROM agente_propuestas WHERE tipo = 'recurrente';
ALTER TABLE agente_propuestas DROP CONSTRAINT agente_propuestas_tipo_valido;
ALTER TABLE agente_propuestas ADD CONSTRAINT agente_propuestas_tipo_valido
    CHECK (tipo IN ('movimiento', 'marcar_pagado', 'abono'));
