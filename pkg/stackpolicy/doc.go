// Package stackpolicy evaluates captured stack declarations against a
// versioned offline policy file selected by gridctl validate --policy.
//
// It does not authorize deployment, inspect runtime state, or change
// ordinary validate, apply, REST, or reload behavior.
package stackpolicy
