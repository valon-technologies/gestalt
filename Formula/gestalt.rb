# frozen_string_literal: true

class Gestalt < Formula
  desc "CLI for Gestalt API - authentication, integration management, and operation invocation"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.7"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.7/gestalt-macos-arm64.tar.gz"
      sha256 "b30e95339ef602ee1ea24ab5195f1c4f1d39fbba9127cb540e3880aaa5ac3e42"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.7/gestalt-macos-x86_64.tar.gz"
      sha256 "482bf7cd0bf3f558b61d9a82a2da1b6d95e7974d7204ab980e0e3c84ee3b7c3b"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.7/gestalt-linux-arm64.tar.gz"
      sha256 "b84bc71b2ad81d13594c71295b9199399282da89b319f8f63cf4443a3c659570"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestalt/v0.0.2-alpha.7/gestalt-linux-x86_64.tar.gz"
      sha256 "fda14cbdb4173cc83b41b3529d6e23eda8b636c1d0c2213be39be7b88a80f34d"
    end
  end

  def install
    bin.install "gestalt"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestalt --version")
  end
end
