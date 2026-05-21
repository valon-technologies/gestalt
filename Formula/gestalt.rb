# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.1"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.1/gestalt-macos-arm64.tar.gz"
      sha256 "74f512d2d0476f79f17c8196d6b5747878761b27401e957add47d6e01ebd1685"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.1/gestalt-macos-x86_64.tar.gz"
      sha256 "8af90094cf46346ec9ddc0f07be1ba4363f27f5b9b90e13b263179d81e817146"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.1/gestalt-linux-arm64.tar.gz"
      sha256 "98e24d1538347a8eec2d5db81a9da73870a84f57151b1d2618e78bac43212fbe"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.1/gestalt-linux-x86_64.tar.gz"
      sha256 "d5dda6cf0b4742662b59794a234cfe0fcc047192c2a583cb267a4d0528a51c38"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
