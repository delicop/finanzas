package agente

import (
	"errors"
	"net/http"

	"finanzas/internal/httpx"
)

type terminarResponse struct {
	// Guardada dice si habia algo que guardar: un chat vacio se descarta.
	Guardada bool `json:"guardada"`
}

// Terminar cierra la conversacion abierta y la deja guardada. El siguiente
// mensaje empieza un hilo nuevo, y el modelo ya no relee el anterior.
func (h *Handler) Terminar(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := h.usuarioPropio(w, r)
	if !ok {
		return
	}

	guardada, err := h.store.Terminar(r.Context(), usuarioID)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "agente: terminando la conversacion")
		return
	}
	httpx.JSON(w, http.StatusOK, terminarResponse{Guardada: guardada})
}

// Guardadas lista las conversaciones terminadas.
func (h *Handler) Guardadas(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := h.usuarioPropio(w, r)
	if !ok {
		return
	}

	lista, err := h.store.Guardadas(r.Context(), usuarioID)
	if err != nil {
		httpx.ErrorInterno(w, r, err, "agente: listando guardadas")
		return
	}
	httpx.JSON(w, http.StatusOK, lista)
}

// Guardada devuelve una conversacion terminada con sus mensajes.
func (h *Handler) Guardada(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := h.usuarioPropio(w, r)
	if !ok {
		return
	}
	id, err := idDeRuta(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Id inválido")
		return
	}

	g, err := h.store.Guardada(r.Context(), usuarioID, id)
	if errors.Is(err, ErrNoEncontrada) {
		// La de otro usuario tambien cae aqui: 404, no 403, para no confirmar
		// que ese id existe.
		httpx.Error(w, http.StatusNotFound, "Conversación no encontrada")
		return
	}
	if err != nil {
		httpx.ErrorInterno(w, r, err, "agente: leyendo guardada")
		return
	}
	httpx.JSON(w, http.StatusOK, g)
}

// BorrarGuardada elimina una conversacion terminada.
func (h *Handler) BorrarGuardada(w http.ResponseWriter, r *http.Request) {
	usuarioID, ok := h.usuarioPropio(w, r)
	if !ok {
		return
	}
	id, err := idDeRuta(r)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Id inválido")
		return
	}

	err = h.store.BorrarGuardada(r.Context(), usuarioID, id)
	if errors.Is(err, ErrNoEncontrada) {
		httpx.Error(w, http.StatusNotFound, "Conversación no encontrada")
		return
	}
	if err != nil {
		httpx.ErrorInterno(w, r, err, "agente: borrando guardada")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
