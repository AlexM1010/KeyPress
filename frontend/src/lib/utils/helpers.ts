// Small shared helpers.
//
// The date/duration helpers that used to live here only ever fed the portfolio
// template's project cards, and went when those did.

export const useTitle = (title: string, suffix: string): string => {
	return `${title} | ${suffix}`;
};
