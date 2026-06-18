// Package selector implements the call-selector framework: the Selector
// interface, a name->constructor registry (replacing Python's dynamic plugin
// import), the shared selection logic (SNR/distance coefficient, sorting,
// blacklist and LOTW filtering) and the individual selector plugins.
//
// Port of plugins/. See FT8CoPilot-rxn.9 and FT8CoPilot-rxn.10.
package selector
