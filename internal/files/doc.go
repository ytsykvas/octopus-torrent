// Package files is where the application decides what to believe about payload files it did not
// touch: whether the bytes a previous run left on disk may be trusted without hashing them again. It
// imports no engine package. Task 009 prototypes the rule here; 020 adds path validation and 029
// wires the rule into startup.
package files
