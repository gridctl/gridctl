import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';

// Mock the API module
vi.mock('../lib/api', () => ({
  storeToken: vi.fn(),
  clearToken: vi.fn(),
  fetchStatus: vi.fn(),
}));

import { AuthPrompt } from '../components/auth/AuthPrompt';
import { useAuthStore } from '../stores/useAuthStore';
import { storeToken, clearToken, fetchStatus } from '../lib/api';

beforeEach(() => {
  vi.clearAllMocks();
  useAuthStore.setState({ authRequired: true, isAuthenticated: false });
});

describe('AuthPrompt', () => {
  it('renders authentication required heading', () => {
    render(<AuthPrompt />);
    expect(screen.getByText('Authentication Required')).toBeInTheDocument();
  });

  it('renders token input and submit button', () => {
    render(<AuthPrompt />);
    expect(screen.getByPlaceholderText('Enter your API token')).toBeInTheDocument();
    expect(screen.getByText('Authenticate')).toBeInTheDocument();
  });

  it('disables submit when token is empty', () => {
    render(<AuthPrompt />);
    const button = screen.getByText('Authenticate');
    expect(button).toBeDisabled();
  });

  it('enables submit when token is entered', () => {
    render(<AuthPrompt />);
    const input = screen.getByPlaceholderText('Enter your API token');
    fireEvent.change(input, { target: { value: 'my-token' } });
    expect(screen.getByText('Authenticate')).not.toBeDisabled();
  });

  it('submits token and authenticates on success', async () => {
    vi.mocked(fetchStatus).mockResolvedValueOnce({} as never);

    render(<AuthPrompt />);
    const input = screen.getByPlaceholderText('Enter your API token');
    fireEvent.change(input, { target: { value: 'valid-token' } });
    fireEvent.submit(input.closest('form')!);

    await waitFor(() => {
      expect(storeToken).toHaveBeenCalledWith('valid-token');
      expect(fetchStatus).toHaveBeenCalled();
    });

    expect(useAuthStore.getState().isAuthenticated).toBe(true);
  });

  it('shows error on invalid credentials', async () => {
    vi.mocked(fetchStatus).mockRejectedValueOnce(new Error('Unauthorized'));

    render(<AuthPrompt />);
    const input = screen.getByPlaceholderText('Enter your API token');
    fireEvent.change(input, { target: { value: 'bad-token' } });
    fireEvent.submit(input.closest('form')!);

    await waitFor(() => {
      expect(screen.getByText('Invalid token — authentication failed')).toBeInTheDocument();
    });

    expect(clearToken).toHaveBeenCalled();
    expect(useAuthStore.getState().isAuthenticated).toBe(false);
  });

  it('toggles token visibility', () => {
    render(<AuthPrompt />);
    const input = screen.getByPlaceholderText('Enter your API token');
    expect(input).toHaveAttribute('type', 'password');

    // Find the toggle button (eye icon button)
    const toggleButtons = screen.getAllByRole('button');
    const toggleButton = toggleButtons.find(b => b.getAttribute('type') === 'button');
    fireEvent.click(toggleButton!);

    expect(input).toHaveAttribute('type', 'text');
  });
});
